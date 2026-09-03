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
	lifetime := int64(1_234_567)
	view := &usageView{
		Status:    "fresh",
		FetchedAt: now.Add(-10 * time.Second),
		PlanType:  "pro",
		RateLimits: &codexusage.RateLimits{RateLimitsByLimitID: map[string]codexusage.RateLimitSnapshot{
			"codex": {
				Primary:   &codexusage.RateLimitWindow{UsedPercent: 21, WindowDurationMins: &primaryDuration},
				Secondary: &codexusage.RateLimitWindow{UsedPercent: 81, WindowDurationMins: &secondaryDuration},
			},
		}},
		TokenUsage: &codexusage.TokenUsage{Summary: codexusage.TokenUsageSummary{LifetimeTokens: &lifetime}},
	}
	plan, limits, tokens, updated := summarizeUsage(view, now)
	if plan != "pro" || limits != "5h 21% · 7d 81%" || tokens != "1.2M" || updated != "just now" {
		t.Fatalf("unexpected summary: %q %q %q %q", plan, limits, tokens, updated)
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
	name := "Codex"
	view := accountView{
		Alias:  "work",
		Email:  "person@example.com",
		Active: true,
		Usage: &usageView{
			Status:    "fresh",
			FetchedAt: now,
			PlanType:  "plus",
			RateLimits: &codexusage.RateLimits{RateLimitsByLimitID: map[string]codexusage.RateLimitSnapshot{
				"codex": {LimitName: &name, Primary: &codexusage.RateLimitWindow{UsedPercent: 33, WindowDurationMins: &duration, ResetsAt: &reset}},
			}},
		},
	}
	output := formatUsage(view, now)
	for _, expected := range []string{"work (person@example.com) [active]", "Plan: plus", "Codex: 5h 33% used, resets in 2h 15m"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output %q does not contain %q", output, expected)
		}
	}
}
