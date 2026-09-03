package app

import (
	"strings"
	"testing"
	"time"

	"github.com/SilkageNet/codex-switch/internal/codexusage"
	"github.com/SilkageNet/codex-switch/internal/switcher"
)

func TestSummarizeUsage(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	primaryDuration := int64(300)
	secondaryDuration := int64(10080)
	primaryReset := now.Add(2*time.Hour + 15*time.Minute).Unix()
	secondaryReset := now.Add(3*24*time.Hour + 4*time.Hour).Unix()
	creditExpiry := now.Add(26 * time.Hour).Unix()
	lifetime := int64(1_234_567)
	view := &usageView{
		Status:    "fresh",
		FetchedAt: now.Add(-10 * time.Second),
		PlanType:  "pro",
		RateLimits: &codexusage.RateLimits{RateLimitsByLimitID: map[string]codexusage.RateLimitSnapshot{
			"codex": {
				Primary:   &codexusage.RateLimitWindow{UsedPercent: 21, WindowDurationMins: &primaryDuration, ResetsAt: &primaryReset},
				Secondary: &codexusage.RateLimitWindow{UsedPercent: 81, WindowDurationMins: &secondaryDuration, ResetsAt: &secondaryReset},
			},
		}, RateLimitResetCredits: &codexusage.RateLimitResetCreditsSummary{
			AvailableCount: 2,
			Credits:        []codexusage.RateLimitResetCredit{{Status: "available", ExpiresAt: &creditExpiry}},
		}},
		TokenUsage: &codexusage.TokenUsage{Summary: codexusage.TokenUsageSummary{LifetimeTokens: &lifetime}},
	}
	plan, limits, resets, tokens, updated := summarizeUsage(view, now)
	if plan != "pro" || limits != "5h 21% ↻2h 15m · 7d 81% ↻3d 4h" || resets != "2 exp 1d 2h" || tokens != "1.2M" || updated != "just now" {
		t.Fatalf("unexpected summary: %q %q %q %q %q", plan, limits, resets, tokens, updated)
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
					"codex": {LimitName: &name, Primary: &codexusage.RateLimitWindow{UsedPercent: 33, WindowDurationMins: &duration, ResetsAt: &reset}},
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
