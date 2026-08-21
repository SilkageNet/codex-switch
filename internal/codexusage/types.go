package codexusage

import "time"

type Snapshot struct {
	FetchedAt  time.Time   `json:"fetchedAt"`
	PlanType   string      `json:"planType,omitempty"`
	RateLimits *RateLimits `json:"rateLimits,omitempty"`
	TokenUsage *TokenUsage `json:"tokenUsage,omitempty"`
	Partial    []string    `json:"partial,omitempty"`
}

type AccountResponse struct {
	Account            *Account `json:"account"`
	RequiresOpenAIAuth bool     `json:"requiresOpenaiAuth"`
}

type Account struct {
	Type     string  `json:"type"`
	Email    *string `json:"email,omitempty"`
	PlanType string  `json:"planType,omitempty"`
}

type RateLimits struct {
	RateLimits            RateLimitSnapshot             `json:"rateLimits"`
	RateLimitsByLimitID   map[string]RateLimitSnapshot  `json:"rateLimitsByLimitId,omitempty"`
	RateLimitResetCredits *RateLimitResetCreditsSummary `json:"rateLimitResetCredits,omitempty"`
}

type RateLimitSnapshot struct {
	LimitID              *string                    `json:"limitId,omitempty"`
	LimitName            *string                    `json:"limitName,omitempty"`
	PlanType             *string                    `json:"planType,omitempty"`
	Primary              *RateLimitWindow           `json:"primary,omitempty"`
	Secondary            *RateLimitWindow           `json:"secondary,omitempty"`
	Credits              *CreditsSnapshot           `json:"credits,omitempty"`
	IndividualLimit      *SpendControlLimitSnapshot `json:"individualLimit,omitempty"`
	SpendControlReached  *bool                      `json:"spendControlReached,omitempty"`
	RateLimitReachedType *string                    `json:"rateLimitReachedType,omitempty"`
}

type RateLimitWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins,omitempty"`
	ResetsAt           *int64 `json:"resetsAt,omitempty"`
}

type CreditsSnapshot struct {
	HasCredits bool    `json:"hasCredits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance,omitempty"`
}

type SpendControlLimitSnapshot struct {
	Limit            string `json:"limit"`
	Used             string `json:"used"`
	RemainingPercent int    `json:"remainingPercent"`
	ResetsAt         int64  `json:"resetsAt"`
}

type RateLimitResetCreditsSummary struct {
	AvailableCount int64                  `json:"availableCount"`
	Credits        []RateLimitResetCredit `json:"credits,omitempty"`
}

type RateLimitResetCredit struct {
	ID          string  `json:"id"`
	ResetType   string  `json:"resetType"`
	Status      string  `json:"status"`
	GrantedAt   int64   `json:"grantedAt"`
	ExpiresAt   *int64  `json:"expiresAt,omitempty"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}

type TokenUsage struct {
	Summary           TokenUsageSummary  `json:"summary"`
	DailyUsageBuckets []DailyUsageBucket `json:"dailyUsageBuckets,omitempty"`
}

type TokenUsageSummary struct {
	LifetimeTokens        *int64 `json:"lifetimeTokens,omitempty"`
	PeakDailyTokens       *int64 `json:"peakDailyTokens,omitempty"`
	LongestRunningTurnSec *int64 `json:"longestRunningTurnSec,omitempty"`
	CurrentStreakDays     *int64 `json:"currentStreakDays,omitempty"`
	LongestStreakDays     *int64 `json:"longestStreakDays,omitempty"`
}

type DailyUsageBucket struct {
	StartDate string `json:"startDate"`
	Tokens    int64  `json:"tokens"`
}

func (snapshot Snapshot) MainRateLimit() *RateLimitSnapshot {
	if snapshot.RateLimits == nil {
		return nil
	}
	if limit, ok := snapshot.RateLimits.RateLimitsByLimitID["codex"]; ok {
		copy := limit
		return &copy
	}
	copy := snapshot.RateLimits.RateLimits
	return &copy
}
