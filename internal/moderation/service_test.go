package moderation

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu         sync.Mutex
	snapshot   *Snapshot
	status     *UserStatus
	rate       [3]int64
	duplicates map[string]int64
	recipients map[string]struct{}
	mute       *Restriction
	ban        *Restriction
	events     []Event
}

func newFakeStore(config Config) *fakeStore {
	return &fakeStore{snapshot: &Snapshot{Config: config}, status: &UserStatus{}, duplicates: make(map[string]int64), recipients: make(map[string]struct{})}
}

func (f *fakeStore) LoadSnapshot(context.Context) (*Snapshot, error)         { return f.snapshot, nil }
func (f *fakeStore) Subscribe(context.Context, func())                       {}
func (f *fakeStore) UserStatus(context.Context, string) (*UserStatus, error) { return f.status, nil }
func (f *fakeStore) RateCounts(context.Context, string) ([3]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rate[0]++
	f.rate[1]++
	f.rate[2]++
	return f.rate, nil
}
func (f *fakeStore) IncrementDuplicate(_ context.Context, _, hash string, _ time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.duplicates[hash]++
	return f.duplicates[hash], nil
}
func (f *fakeStore) TrackRecipient(_ context.Context, _, recipient string, _ time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recipients[recipient] = struct{}{}
	return int64(len(f.recipients)), nil
}
func (f *fakeStore) SetMute(_ context.Context, _ string, restriction Restriction, _ time.Duration) error {
	f.mute = &restriction
	return nil
}
func (f *fakeStore) SetBan(_ context.Context, _ string, restriction Restriction, _ time.Duration) error {
	f.ban = &restriction
	return nil
}
func (f *fakeStore) AppendEvent(_ context.Context, event Event) error {
	f.events = append(f.events, event)
	return nil
}

func TestRateLimitAndDuplicateDetection(t *testing.T) {
	config := DefaultConfig()
	store := newFakeStore(config)
	service, err := NewService(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	var decision Decision
	for i := 0; i < 9; i++ {
		decision, err = service.CheckMessage(context.Background(), Message{UserID: "u1", RecipientID: "u2", Text: "messaggio " + string(rune('a'+i))})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !containsString(decision.Reasons, reasonRate10S) {
		t.Fatalf("expected rate-limit reason, got %v", decision.Reasons)
	}

	for i := 0; i < 6; i++ {
		decision, err = service.CheckMessage(context.Background(), Message{UserID: "duplicate-user", RecipientID: "u2", Text: "sempre uguale"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !containsString(decision.Reasons, reasonDuplicate) || decision.Score < 40 {
		t.Fatalf("expected duplicate score, got %#v", decision)
	}
}

func TestMassRecipients(t *testing.T) {
	store := newFakeStore(DefaultConfig())
	service, _ := NewService(context.Background(), store)
	var decision Decision
	for i := 0; i < 20; i++ {
		decision, _ = service.CheckMessage(context.Background(), Message{UserID: "u1", RecipientID: string(rune('a' + i)), Text: "testo diverso " + string(rune('a'+i))})
	}
	if !containsString(decision.Reasons, reasonMassRecipients) || decision.Score < 50 {
		t.Fatalf("expected mass recipient block, got %#v", decision)
	}
}

func TestBannedWordBlocks(t *testing.T) {
	store := newFakeStore(DefaultConfig())
	store.snapshot.BannedTerms = []BannedTerm{{ID: "1", Term: "viagra", Severity: 60, Enabled: true, Action: ActionBlock}}
	service, _ := NewService(context.Background(), store)
	decision, err := service.CheckMessage(context.Background(), Message{UserID: "u1", RecipientID: "u2", Text: "V.I.A.G.R.A"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Allowed || decision.Action != ActionBlock || len(store.events) != 1 {
		t.Fatalf("expected blocked and audited message, got %#v events=%d", decision, len(store.events))
	}
}
