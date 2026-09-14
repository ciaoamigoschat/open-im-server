package moderation

import "time"

type Action string

const (
	ActionAllow    Action = "ALLOW"
	ActionAllowLog Action = "ALLOW_LOG"
	ActionBlock    Action = "BLOCK"
	ActionMute     Action = "MUTE"
	ActionTempBan  Action = "TEMP_BAN"
	ActionReview   Action = "REVIEW"
)

type RedisFailurePolicy string

const (
	FailOpen  RedisFailurePolicy = "FAIL_OPEN"
	FailClose RedisFailurePolicy = "FAIL_CLOSE"
)

type BannedTerm struct {
	ID        string `json:"id"`
	Term      string `json:"term"`
	Category  string `json:"category"`
	Severity  int    `json:"severity"`
	ExactOnly bool   `json:"exactOnly"`
	Fuzzy     bool   `json:"fuzzy"`
	Enabled   bool   `json:"enabled"`
	Action    Action `json:"action"`
}

type BannedDomain struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Severity int    `json:"severity"`
	Action   Action `json:"action"`
	Enabled  bool   `json:"enabled"`
}

type ScoreLevel struct {
	Count int `json:"count"`
	Score int `json:"score"`
}

type RateLimitConfig struct {
	Messages10Sec int `json:"messages10Sec"`
	Messages60Sec int `json:"messages60Sec"`
	Messages5Min  int `json:"messages5Min"`
}

type DuplicateConfig struct {
	WindowSeconds int          `json:"windowSeconds"`
	WarningCount  int          `json:"warningCount"`
	BlockCount    int          `json:"blockCount"`
	Levels        []ScoreLevel `json:"levels"`
}

type RecipientConfig struct {
	WindowSeconds int          `json:"windowSeconds"`
	WarningCount  int          `json:"warningCount"`
	BlockCount    int          `json:"blockCount"`
	Levels        []ScoreLevel `json:"levels"`
}

type FuzzyConfig struct {
	Enabled           bool `json:"enabled"`
	MinWordLength     int  `json:"minWordLength"`
	MaxDistanceMedium int  `json:"maxDistanceMedium"`
	MaxDistanceLong   int  `json:"maxDistanceLong"`
}

type ScoreConfig struct {
	Flood10Sec          int `json:"flood10Sec"`
	Flood60Sec          int `json:"flood60Sec"`
	Flood5Min           int `json:"flood5Min"`
	SuspiciousLink      int `json:"suspiciousLink"`
	PhoneSolicitation   int `json:"phoneSolicitation"`
	DuplicateRecipients int `json:"duplicateRecipients"`
}

type ThresholdConfig struct {
	Log            int `json:"log"`
	Block          int `json:"block"`
	Mute5Minutes   int `json:"mute5Minutes"`
	Mute1Hour      int `json:"mute1Hour"`
	Mute24Hours    int `json:"mute24Hours"`
	Review         int `json:"review"`
	TempBanSeconds int `json:"tempBanSeconds"`
}

type Config struct {
	Enabled       bool               `json:"enabled"`
	FailurePolicy RedisFailurePolicy `json:"redisFailurePolicy"`
	RateLimit     RateLimitConfig    `json:"rateLimit"`
	Duplicate     DuplicateConfig    `json:"duplicate"`
	Recipients    RecipientConfig    `json:"recipients"`
	FuzzyMatching FuzzyConfig        `json:"fuzzyMatching"`
	Score         ScoreConfig        `json:"score"`
	Thresholds    ThresholdConfig    `json:"thresholds"`
}

type Snapshot struct {
	Version       int64          `json:"version"`
	Config        Config         `json:"config"`
	BannedTerms   []BannedTerm   `json:"bannedWords"`
	BannedDomains []BannedDomain `json:"bannedDomains"`
}

type Message struct {
	UserID         string
	RecipientID    string
	ConversationID string
	MessageID      string
	Text           string
}

type Decision struct {
	Allowed      bool     `json:"allowed"`
	Score        int      `json:"score"`
	Action       Action   `json:"action"`
	Reasons      []string `json:"reasons"`
	MatchedTerms []string `json:"matchedTerms,omitempty"`
	MuteSeconds  int64    `json:"muteSeconds,omitempty"`
}

type Restriction struct {
	Reason    string    `json:"reason,omitempty"`
	ActorID   string    `json:"actorID,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

type UserStatus struct {
	UserID string       `json:"userID"`
	Muted  *Restriction `json:"mute,omitempty"`
	Banned *Restriction `json:"ban,omitempty"`
}

type Event struct {
	ID              string         `json:"id"`
	UserID          string         `json:"userID"`
	ConversationID  string         `json:"conversationID,omitempty"`
	MessageID       string         `json:"messageID,omitempty"`
	Score           int            `json:"score"`
	Action          Action         `json:"action"`
	Reasons         []string       `json:"reasons"`
	MatchedTerms    []string       `json:"matchedTerms,omitempty"`
	MatchedDomains  []string       `json:"matchedDomains,omitempty"`
	Technical       map[string]int `json:"technical,omitempty"`
	DurationSeconds int64          `json:"durationSeconds,omitempty"`
	ReviewStatus    string         `json:"reviewStatus,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
}

type EventPage struct {
	Events   []Event `json:"events"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
	Total    int64   `json:"total"`
}
