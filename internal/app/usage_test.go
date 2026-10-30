package app

import (
	"strings"
	"testing"
	"time"

	"github.com/SilkageNet/codex-switch/internal/codexusage"
	"github.com/SilkageNet/codex-switch/internal/switcher"
	"github.com/SilkageNet/codex-switch/internal/usagecache"
)

func TestSummarizeUsage(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	primaryDuration := int64(300)
	secondaryDuration := int64(10080)
	primaryReset := now.Add(2*time.Hour + 15*time.Minute).Unix()
	secondaryReset := now.Add(3*24*time.Hour + 4*time.Hour).Unix()
	creditExpiry := now.Add(26 * time.Hour).Unix()
	lifetime := int64(1_234_567)
	balance := "61287.1250"
	view := &usageView{
		Status:    "fresh",
		FetchedAt: now.Add(-10 * time.Second),
		PlanType:  "pro",
		RateLimits: &codexusage.RateLimits{RateLimitsByLimitID: map[string]codexusage.RateLimitSnapshot{
			"codex": {
				Primary:   &codexusage.RateLimitWindow{UsedPercent: 21, WindowDurationMins: &primaryDuration, ResetsAt: &primaryReset},
				Secondary: &codexusage.RateLimitWindow{UsedPercent: 81, WindowDurationMins: &secondaryDuration, ResetsAt: &secondaryReset},
				Credits:   &codexusage.CreditsSnapshot{HasCredits: true, Balance: &balance},
			},
		}, RateLimitResetCredits: &codexusage.RateLimitResetCreditsSummary{
			AvailableCount: 2,
			Credits:        []codexusage.RateLimitResetCredit{{Status: "available", ExpiresAt: &creditExpiry}},
		}},
		TokenUsage: &codexusage.TokenUsage{Summary: codexusage.TokenUsageSummary{LifetimeTokens: &lifetime}},
	}
	plan, limits, credits, resets, tokens, updated := summarizeUsage(view, now)
	if plan != "pro" || limits != "5h 21% ↻2h 15m · 7d 81% ↻3d 4h" || credits != "61,287.12" || resets != "2 exp 1d 2h" || tokens != "1.2M" || updated != "just now" {
		t.Fatalf("unexpected summary: %q %q %q %q %q %q", plan, limits, credits, resets, tokens, updated)
	}
}

func TestUsageFromCacheRetainsLastRefreshFailure(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	cache := usagecache.Cache{
		Profiles: map[string]codexusage.Snapshot{"profile-a": {FetchedAt: now.Add(-2 * time.Minute), PlanType: "pro"}},
		Failures: map[string]usagecache.Failure{"profile-a": {AttemptedAt: now.Add(-time.Minute), Message: "temporary connection failure", Attempts: 3}},
	}
	view := usageFromCache(cache, "profile-a", "", now)
	if view.Status != "fresh" || view.Error != "temporary connection failure" || view.Attempts != 3 {
		t.Fatalf("unexpected cached usage view: %#v", view)
	}
	if got := compactUsageError(view); got != "temporary connection failure" {
		t.Fatalf("compact error = %q", got)
	}
	if usageStatus(now.Add(-6*time.Minute), now) != "stale" {
		t.Fatal("snapshot older than five minutes was not stale")
	}
}

func TestObservationMessagesExplainExternalLogin(t *testing.T) {
	observation := switcher.Observation{
		State:         switcher.AccountStateExternalLoginWithRefresh,
		Alias:         "silkage",
		Email:         "silkage@example.com",
		RecordedAlias: "kun",
	}
	status := formatObservation(observation)
	notice := observationNotice(observation)
	for _, expected := range []string{"silkage", "outside codex-switch", "codex-switch sync"} {
		if !strings.Contains(status, expected) {
			t.Fatalf("status %q does not contain %q", status, expected)
		}
	}
	for _, expected := range []string{"silkage", "kun", "codex-switch sync"} {
		if !strings.Contains(notice, expected) {
			t.Fatalf("notice %q does not contain %q", notice, expected)
		}
	}
}

func TestFormatUsageShowsDetailedWindows(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	duration := int64(300)
	reset := now.Add(2*time.Hour + 15*time.Minute).Unix()
	granted := now.Add(-24 * time.Hour).Unix()
	expires := now.Add(72 * time.Hour).Unix()
	name := "Codex"
	title := "Rate-limit reset"
	balance := "61287.1250"
	view := accountView{
		Alias:  "work",
		Email:  "person@example.com",
		Active: true,
		Usage: &usageView{
			Status:    "fresh",
			FetchedAt: now,
			PlanType:  "plus",
			RateLimits: &codexusage.RateLimits{
				RateLimitsByLimitID: map[string]codexusage.RateLimitSnapshot{
					"codex": {
						LimitName: &name,
						Primary:   &codexusage.RateLimitWindow{UsedPercent: 33, WindowDurationMins: &duration, ResetsAt: &reset},
						Credits:   &codexusage.CreditsSnapshot{HasCredits: true, Balance: &balance},
					},
				},
				RateLimitResetCredits: &codexusage.RateLimitResetCreditsSummary{
					AvailableCount: 2,
					Credits: []codexusage.RateLimitResetCredit{{
						ResetType: "codexRateLimits",
						Status:    "available",
						GrantedAt: granted,
						ExpiresAt: &expires,
						Title:     &title,
					}},
				},
			},
		},
	}
	output := formatUsage(view, now)
	for _, expected := range []string{
		"work (person@example.com) [active]",
		"Plan: plus",
		"Codex: 5h 33% used, resets in 2h 15m at " + formatLocalTime(time.Unix(reset, 0)),
		"Credits: 61,287.12 available",
		"Earned resets: 2 available",
		"Rate-limit reset: available; expires in 3d 0h at " + formatLocalTime(time.Unix(expires, 0)),
		"granted " + formatLocalTime(time.Unix(granted, 0)),
		"Details: Codex returned 1 of 2 available resets",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestCompactCreditsDistinguishesStates(t *testing.T) {
	balance := "46476.3720375000"
	tests := []struct {
		name  string
		limit *codexusage.RateLimitSnapshot
		want  string
	}{
		{name: "unavailable", want: "-"},
		{name: "none", limit: &codexusage.RateLimitSnapshot{Credits: &codexusage.CreditsSnapshot{}}, want: "0"},
		{name: "available without balance", limit: &codexusage.RateLimitSnapshot{Credits: &codexusage.CreditsSnapshot{HasCredits: true}}, want: "available"},
		{name: "balance", limit: &codexusage.RateLimitSnapshot{Credits: &codexusage.CreditsSnapshot{HasCredits: true, Balance: &balance}}, want: "46,476.37"},
		{name: "unlimited", limit: &codexusage.RateLimitSnapshot{Credits: &codexusage.CreditsSnapshot{Unlimited: true}}, want: "unlimited"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := compactCredits(test.limit); got != test.want {
				t.Fatalf("compactCredits() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormatUsageDistinguishesUnavailableResetDetails(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	view := accountView{Alias: "work", Usage: &usageView{
		Status:   "fresh",
		PlanType: "pro",
		RateLimits: &codexusage.RateLimits{RateLimitResetCredits: &codexusage.RateLimitResetCreditsSummary{
			AvailableCount: 1,
		}},
	}}
	output := formatUsage(view, now)
	for _, expected := range []string{"Earned resets: 1 available", "Details: Codex returned 0 of 1 available resets"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}

func TestCompactResetCreditsDistinguishesUnavailableAndZero(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	if value := compactResetCredits(nil, now); value != "-" {
		t.Fatalf("unavailable reset credits = %q", value)
	}
	limits := &codexusage.RateLimits{RateLimitResetCredits: &codexusage.RateLimitResetCreditsSummary{}}
	if value := compactResetCredits(limits, now); value != "0" {
		t.Fatalf("zero reset credits = %q", value)
	}
}
