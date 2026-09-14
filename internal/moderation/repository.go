package moderation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix        = "moderation:"
	configKey        = keyPrefix + "{global}:config"
	configVersionKey = keyPrefix + "{global}:config:version"
	bannedWordsKey   = keyPrefix + "{global}:banned_words"
	bannedDomainsKey = keyPrefix + "{global}:banned_domains"
	reloadChannel    = keyPrefix + "config:reload"
	eventsKey        = keyPrefix + "{global}:events"
	statsKey         = keyPrefix + "{global}:stats"
)

type Store interface {
	LoadSnapshot(context.Context) (*Snapshot, error)
	Subscribe(context.Context, func())
	UserStatus(context.Context, string) (*UserStatus, error)
	RateCounts(context.Context, string) ([3]int64, error)
	IncrementDuplicate(context.Context, string, string, time.Duration) (int64, error)
	TrackRecipient(context.Context, string, string, time.Duration) (int64, error)
	SetMute(context.Context, string, Restriction, time.Duration) error
	SetBan(context.Context, string, Restriction, time.Duration) error
	AppendEvent(context.Context, Event) error
}

type EventFilter struct {
	Page     int
	PageSize int
	UserID   string
	Action   Action
	Reason   string
}

type Repository struct {
	client redis.UniversalClient
}

func NewRepository(client redis.UniversalClient) *Repository {
	return &Repository{client: client}
}

func (r *Repository) Initialize(ctx context.Context) error {
	config := DefaultConfig()
	payload, err := json.Marshal(config)
	if err != nil {
		return err
	}
	created, err := r.client.SetNX(ctx, configKey, payload, 0).Result()
	if err != nil {
		return err
	}
	if created {
		return r.client.SetNX(ctx, configVersionKey, 1, 0).Err()
	}
	return nil
}

func (r *Repository) LoadSnapshot(ctx context.Context) (*Snapshot, error) {
	if err := r.Initialize(ctx); err != nil {
		return nil, err
	}
	pipe := r.client.Pipeline()
	configCmd := pipe.Get(ctx, configKey)
	versionCmd := pipe.Get(ctx, configVersionKey)
	wordsCmd := pipe.HVals(ctx, bannedWordsKey)
	domainsCmd := pipe.HVals(ctx, bannedDomainsKey)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	snapshot := &Snapshot{}
	if err := json.Unmarshal([]byte(configCmd.Val()), &snapshot.Config); err != nil {
		return nil, fmt.Errorf("decode moderation config: %w", err)
	}
	snapshot.Config.ApplyDefaults()
	snapshot.Version, _ = strconv.ParseInt(versionCmd.Val(), 10, 64)
	if err := decodeList(wordsCmd.Val(), &snapshot.BannedTerms); err != nil {
		return nil, fmt.Errorf("decode banned words: %w", err)
	}
	if err := decodeList(domainsCmd.Val(), &snapshot.BannedDomains); err != nil {
		return nil, fmt.Errorf("decode banned domains: %w", err)
	}
	sort.Slice(snapshot.BannedTerms, func(i, j int) bool { return snapshot.BannedTerms[i].ID < snapshot.BannedTerms[j].ID })
	sort.Slice(snapshot.BannedDomains, func(i, j int) bool { return snapshot.BannedDomains[i].ID < snapshot.BannedDomains[j].ID })
	return snapshot, nil
}

func decodeList[T any](values []string, target *[]T) error {
	for _, value := range values {
		var item T
		if err := json.Unmarshal([]byte(value), &item); err != nil {
			return err
		}
		*target = append(*target, item)
	}
	return nil
}

func (r *Repository) SaveConfig(ctx context.Context, config Config) (int64, error) {
	if err := config.Validate(); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(config)
	if err != nil {
		return 0, err
	}
	var version *redis.IntCmd
	_, err = r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, configKey, payload, 0)
		version = pipe.Incr(ctx, configVersionKey)
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := r.client.Publish(ctx, reloadChannel, version.Val()).Err(); err != nil {
		return 0, err
	}
	return version.Val(), nil
}

func (r *Repository) SaveBannedTerm(ctx context.Context, item BannedTerm) (BannedTerm, error) {
	if item.ID == "" {
		item.ID = newID()
	}
	if item.Action == "" {
		item.Action = ActionBlock
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	if err := r.client.HSet(ctx, bannedWordsKey, item.ID, payload).Err(); err != nil {
		return item, err
	}
	return item, r.publishReload(ctx)
}

func (r *Repository) SaveBannedDomain(ctx context.Context, item BannedDomain) (BannedDomain, error) {
	if item.ID == "" {
		item.ID = newID()
	}
	if item.Action == "" {
		item.Action = ActionBlock
	}
	payload, err := json.Marshal(item)
	if err != nil {
		return item, err
	}
	if err := r.client.HSet(ctx, bannedDomainsKey, item.ID, payload).Err(); err != nil {
		return item, err
	}
	return item, r.publishReload(ctx)
}

func (r *Repository) DeleteBannedTerm(ctx context.Context, id string) error {
	if err := r.client.HDel(ctx, bannedWordsKey, id).Err(); err != nil {
		return err
	}
	return r.publishReload(ctx)
}

func (r *Repository) DeleteBannedDomain(ctx context.Context, id string) error {
	if err := r.client.HDel(ctx, bannedDomainsKey, id).Err(); err != nil {
		return err
	}
	return r.publishReload(ctx)
}

func (r *Repository) publishReload(ctx context.Context) error {
	version, err := r.client.Incr(ctx, configVersionKey).Result()
	if err != nil {
		return err
	}
	return r.client.Publish(ctx, reloadChannel, version).Err()
}

func (r *Repository) Subscribe(ctx context.Context, reload func()) {
	pubsub := r.client.Subscribe(ctx, reloadChannel)
	defer pubsub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-pubsub.Channel():
			if !ok {
				return
			}
			reload()
		}
	}
}

func userKey(userID, suffix string) string {
	return fmt.Sprintf("%suser:{%s}:%s", keyPrefix, userID, suffix)
}

func (r *Repository) UserStatus(ctx context.Context, userID string) (*UserStatus, error) {
	values, err := r.client.MGet(ctx, userKey(userID, "mute"), userKey(userID, "ban")).Result()
	if err != nil {
		return nil, err
	}
	status := &UserStatus{UserID: userID}
	if values[0] != nil {
		status.Muted = &Restriction{}
		if err := json.Unmarshal([]byte(fmt.Sprint(values[0])), status.Muted); err != nil {
			return nil, err
		}
	}
	if values[1] != nil {
		status.Banned = &Restriction{}
		if err := json.Unmarshal([]byte(fmt.Sprint(values[1])), status.Banned); err != nil {
			return nil, err
		}
	}
	return status, nil
}

var rateScript = redis.NewScript(`
local result = {}
for i, key in ipairs(KEYS) do
  local value = redis.call('INCR', key)
  if value == 1 then redis.call('EXPIRE', key, tonumber(ARGV[i])) end
  result[i] = value
end
return result`)

func (r *Repository) RateCounts(ctx context.Context, userID string) ([3]int64, error) {
	keys := []string{userKey(userID, "msg:10s"), userKey(userID, "msg:60s"), userKey(userID, "msg:5m")}
	result, err := rateScript.Run(ctx, r.client, keys, 10, 60, 300).Int64Slice()
	if err != nil {
		return [3]int64{}, err
	}
	return [3]int64{result[0], result[1], result[2]}, nil
}

var incrementTTLScript = redis.NewScript(`
local value = redis.call('INCR', KEYS[1])
if value == 1 then redis.call('EXPIRE', KEYS[1], tonumber(ARGV[1])) end
return value`)

func (r *Repository) IncrementDuplicate(ctx context.Context, userID, hash string, ttl time.Duration) (int64, error) {
	return incrementTTLScript.Run(ctx, r.client, []string{userKey(userID, "duplicate:"+hash)}, int64(ttl.Seconds())).Int64()
}

var recipientScript = redis.NewScript(`
local key, now, window, recipient = KEYS[1], tonumber(ARGV[1]), tonumber(ARGV[2]), ARGV[3]
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
redis.call('ZADD', key, now, recipient)
redis.call('EXPIRE', key, math.ceil(window / 1000) + 1)
return redis.call('ZCARD', key)`)

func (r *Repository) TrackRecipient(ctx context.Context, userID, recipientID string, ttl time.Duration) (int64, error) {
	return recipientScript.Run(ctx, r.client, []string{userKey(userID, "recipients")}, time.Now().UnixMilli(), ttl.Milliseconds(), recipientID).Int64()
}

func (r *Repository) SetMute(ctx context.Context, userID string, restriction Restriction, ttl time.Duration) error {
	return r.setRestriction(ctx, userKey(userID, "mute"), restriction, ttl)
}

func (r *Repository) SetBan(ctx context.Context, userID string, restriction Restriction, ttl time.Duration) error {
	return r.setRestriction(ctx, userKey(userID, "ban"), restriction, ttl)
}

func (r *Repository) setRestriction(ctx context.Context, key string, restriction Restriction, ttl time.Duration) error {
	restriction.CreatedAt = time.Now().UTC()
	if ttl > 0 {
		restriction.ExpiresAt = restriction.CreatedAt.Add(ttl)
	}
	payload, err := json.Marshal(restriction)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, key, payload, ttl).Err()
}

func (r *Repository) DeleteMute(ctx context.Context, userID string) error {
	return r.client.Del(ctx, userKey(userID, "mute")).Err()
}

func (r *Repository) DeleteBan(ctx context.Context, userID string) error {
	return r.client.Del(ctx, userKey(userID, "ban")).Err()
}

func (r *Repository) AppendEvent(ctx context.Context, event Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = r.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.LPush(ctx, eventsKey, payload)
		pipe.LTrim(ctx, eventsKey, 0, 9999)
		pipe.HIncrBy(ctx, statsKey, "action:"+string(event.Action), 1)
		for _, reason := range event.Reasons {
			pipe.HIncrBy(ctx, statsKey, "reason:"+reason, 1)
		}
		return nil
	})
	return err
}

func (r *Repository) Events(ctx context.Context, filter EventFilter) (*EventPage, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	values, err := r.client.LRange(ctx, eventsKey, 0, 9999).Result()
	if err != nil {
		return nil, err
	}
	filtered := make([]Event, 0)
	for _, value := range values {
		var event Event
		if json.Unmarshal([]byte(value), &event) != nil || (filter.UserID != "" && event.UserID != filter.UserID) || (filter.Action != "" && event.Action != filter.Action) || (filter.Reason != "" && !containsString(event.Reasons, filter.Reason)) {
			continue
		}
		filtered = append(filtered, event)
	}
	start := (filter.Page - 1) * filter.PageSize
	end := start + filter.PageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}
	return &EventPage{Events: filtered[start:end], Page: filter.Page, PageSize: filter.PageSize, Total: int64(len(filtered))}, nil
}

func (r *Repository) Stats(ctx context.Context) (map[string]int64, error) {
	values, err := r.client.HGetAll(ctx, statsKey).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(values))
	for key, value := range values {
		result[key], _ = strconv.ParseInt(value, 10, 64)
	}
	return result, nil
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func newID() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(value)
}
