package moderation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/openimsdk/tools/log"
)

const (
	reasonUserMuted         = "USER_MUTED"
	reasonUserBanned        = "USER_BANNED"
	reasonRate10S           = "RATE_LIMIT_10S"
	reasonRate60S           = "RATE_LIMIT_60S"
	reasonRate5M            = "RATE_LIMIT_5M"
	reasonDuplicate         = "DUPLICATE_MESSAGE"
	reasonMassRecipients    = "MASS_RECIPIENTS"
	reasonBannedWord        = "BANNED_WORD"
	reasonBannedDomain      = "BANNED_DOMAIN"
	reasonSuspiciousLink    = "SUSPICIOUS_LINK"
	reasonPhoneSolicitation = "PHONE_SOLICITATION"
	reasonFuzzyWord         = "FUZZY_BANNED_WORD"
	reasonRiskScore         = "RISK_SCORE_LIMIT"
)

type Service struct {
	store    Store
	snapshot atomic.Pointer[Snapshot]
}

func NewService(ctx context.Context, store Store) (*Service, error) {
	service := &Service{store: store}
	snapshot, err := store.LoadSnapshot(ctx)
	if err != nil {
		snapshot = &Snapshot{Config: DefaultConfig()}
	}
	service.snapshot.Store(snapshot)
	go store.Subscribe(ctx, service.reload)
	go service.refreshPeriodically(ctx)
	return service, err
}

func (s *Service) refreshPeriodically(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reload()
		}
	}
}

func (s *Service) reload() {
	snapshot, err := s.store.LoadSnapshot(context.Background())
	if err != nil {
		log.ZError(context.Background(), "moderation config reload failed", err)
		return
	}
	s.snapshot.Store(snapshot)
}

func (s *Service) CheckMessage(ctx context.Context, message Message) (Decision, error) {
	startedAt := time.Now()
	snapshot := s.snapshot.Load()
	config := snapshot.Config
	if !config.Enabled || strings.TrimSpace(message.Text) == "" {
		return Decision{Allowed: true, Action: ActionAllow}, nil
	}

	status, err := s.store.UserStatus(ctx, message.UserID)
	if err != nil {
		return s.redisFailure(config, err)
	}
	if status.Banned != nil {
		return s.restrictedDecision(ctx, message, ActionTempBan, reasonUserBanned, status.Banned)
	}
	if status.Muted != nil {
		return s.restrictedDecision(ctx, message, ActionMute, reasonUserMuted, status.Muted)
	}

	rateStarted := time.Now()
	counts, err := s.store.RateCounts(ctx, message.UserID)
	observeRedis(time.Since(rateStarted))
	if err != nil {
		return s.redisFailure(config, err)
	}
	reasons := make([]string, 0, 10)
	score := 0
	if counts[0] > int64(config.RateLimit.Messages10Sec) {
		score += config.Score.Flood10Sec
		reasons = append(reasons, reasonRate10S)
	}
	if counts[1] > int64(config.RateLimit.Messages60Sec) {
		score += config.Score.Flood60Sec
		reasons = append(reasons, reasonRate60S)
	}
	if counts[2] > int64(config.RateLimit.Messages5Min) {
		score += config.Score.Flood5Min
		reasons = append(reasons, reasonRate5M)
	}

	normalizationStarted := time.Now()
	normalized := NormalizeText(message.Text)
	observeNormalization(time.Since(normalizationStarted))
	rulesStarted := time.Now()
	matchedTerms, termScore, explicitAction, fuzzyMatched := matchBannedTerms(normalized, snapshot.BannedTerms, config.FuzzyMatching)
	if len(matchedTerms) > 0 {
		score += termScore
		reasons = append(reasons, reasonBannedWord)
		if fuzzyMatched {
			reasons = append(reasons, reasonFuzzyWord)
		}
	}

	hashBytes := sha256.Sum256([]byte(normalized.NormalizedText))
	duplicateCount, err := s.store.IncrementDuplicate(ctx, message.UserID, hex.EncodeToString(hashBytes[:]), time.Duration(config.Duplicate.WindowSeconds)*time.Second)
	if err != nil {
		return s.redisFailure(config, err)
	}
	duplicateScore := scoreForCount(duplicateCount, config.Duplicate.Levels)
	if duplicateScore > 0 {
		score += duplicateScore
		reasons = append(reasons, reasonDuplicate)
	}

	recipientCount, err := s.store.TrackRecipient(ctx, message.UserID, message.RecipientID, time.Duration(config.Recipients.WindowSeconds)*time.Second)
	if err != nil {
		return s.redisFailure(config, err)
	}
	recipientScore := scoreForCount(recipientCount, config.Recipients.Levels)
	if recipientScore > 0 {
		score += recipientScore
		reasons = append(reasons, reasonMassRecipients)
	}
	if duplicateScore > 0 && recipientScore > 0 {
		score += config.Score.DuplicateRecipients
	}

	foundDomains := extractDomains(message.Text)
	matchedDomains, domainScore, domainAction := matchBannedDomains(foundDomains, snapshot.BannedDomains)
	if len(matchedDomains) > 0 {
		score += domainScore
		reasons = append(reasons, reasonBannedDomain)
		explicitAction = strongerAction(explicitAction, domainAction)
	}
	if len(foundDomains) > 0 && (solicitationPattern.MatchString(message.Text) || duplicateScore > 0 || recipientScore > 0) {
		score += config.Score.SuspiciousLink
		reasons = append(reasons, reasonSuspiciousLink)
	}
	if hasPhoneSolicitation(message.Text) {
		score += config.Score.PhoneSolicitation
		reasons = append(reasons, reasonPhoneSolicitation)
	}
	observeRules(time.Since(rulesStarted))

	action, duration := actionForScore(score, config.Thresholds)
	action = strongerAction(action, explicitAction)
	if action != ActionAllow && action != ActionAllowLog && !containsString(reasons, reasonRiskScore) {
		reasons = append(reasons, reasonRiskScore)
	}
	if action == ActionMute && duration == 0 {
		duration = 5 * time.Minute
	}
	if action == ActionTempBan && duration == 0 {
		duration = time.Duration(config.Thresholds.TempBanSeconds) * time.Second
	}

	decision := Decision{Allowed: action == ActionAllow || action == ActionAllowLog, Score: score, Action: action, Reasons: reasons, MatchedTerms: matchedTerms, MuteSeconds: int64(duration.Seconds())}
	if err := s.applyAction(ctx, message.UserID, action, duration); err != nil {
		return s.redisFailure(config, err)
	}
	if action != ActionAllow {
		event := Event{
			ID: newID(), UserID: message.UserID, ConversationID: message.ConversationID, MessageID: message.MessageID,
			Score: score, Action: action, Reasons: reasons, MatchedTerms: matchedTerms, MatchedDomains: matchedDomains,
			Technical:       map[string]int{"messages10Sec": int(counts[0]), "messages60Sec": int(counts[1]), "messages5Min": int(counts[2]), "duplicateCount": int(duplicateCount), "uniqueRecipientCount": int(recipientCount)},
			DurationSeconds: int64(duration.Seconds()), CreatedAt: time.Now().UTC(),
		}
		if action == ActionTempBan || action == ActionReview {
			event.ReviewStatus = "PENDING"
		}
		if err := s.store.AppendEvent(ctx, event); err != nil {
			log.ZError(ctx, "moderation event append failed", err, "userID", message.UserID)
		}
	}
	observeCheck(time.Since(startedAt), action)
	return decision, nil
}

func (s *Service) restrictedDecision(ctx context.Context, message Message, action Action, reason string, restriction *Restriction) (Decision, error) {
	duration := time.Duration(0)
	if !restriction.ExpiresAt.IsZero() {
		duration = time.Until(restriction.ExpiresAt)
		if duration < 0 {
			duration = 0
		}
	}
	decision := Decision{Allowed: false, Action: action, Reasons: []string{reason}, MuteSeconds: int64(duration.Seconds())}
	_ = s.store.AppendEvent(ctx, Event{ID: newID(), UserID: message.UserID, ConversationID: message.ConversationID, MessageID: message.MessageID, Action: action, Reasons: decision.Reasons, DurationSeconds: decision.MuteSeconds, CreatedAt: time.Now().UTC()})
	return decision, nil
}

func (s *Service) redisFailure(config Config, err error) (Decision, error) {
	if config.FailurePolicy == FailClose {
		return Decision{Allowed: false, Action: ActionBlock, Reasons: []string{"REDIS_UNAVAILABLE"}}, err
	}
	log.ZError(context.Background(), "moderation redis unavailable; fail open", err)
	return Decision{Allowed: true, Action: ActionAllow, Reasons: []string{"REDIS_UNAVAILABLE"}}, nil
}

func (s *Service) applyAction(ctx context.Context, userID string, action Action, duration time.Duration) error {
	restriction := Restriction{Reason: reasonRiskScore}
	switch action {
	case ActionMute:
		return s.store.SetMute(ctx, userID, restriction, duration)
	case ActionTempBan:
		return s.store.SetBan(ctx, userID, restriction, duration)
	default:
		return nil
	}
}

func actionForScore(score int, thresholds ThresholdConfig) (Action, time.Duration) {
	switch {
	case score >= thresholds.Review:
		return ActionTempBan, time.Duration(thresholds.TempBanSeconds) * time.Second
	case score >= thresholds.Mute24Hours:
		return ActionMute, 24 * time.Hour
	case score >= thresholds.Mute1Hour:
		return ActionMute, time.Hour
	case score >= thresholds.Mute5Minutes:
		return ActionMute, 5 * time.Minute
	case score >= thresholds.Block:
		return ActionBlock, 0
	case score >= thresholds.Log:
		return ActionAllowLog, 0
	default:
		return ActionAllow, 0
	}
}

func (d Decision) Error() error {
	return fmt.Errorf("moderation action %s (score %d): %s", d.Action, d.Score, strings.Join(d.Reasons, ","))
}
