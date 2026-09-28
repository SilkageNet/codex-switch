package usagecache

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/SilkageNet/codex-switch/internal/codexusage"
)

func TestLoadMissingAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "usage.json")
	cache, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cache.Version != 1 || cache.Profiles == nil || cache.Failures == nil {
		t.Fatalf("unexpected empty cache: %#v", cache)
	}
	fetched := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	expires := fetched.Add(30 * 24 * time.Hour).Unix()
	cache.Profiles["profile-a"] = codexusage.Snapshot{
		FetchedAt: fetched,
		PlanType:  "pro",
		RateLimits: &codexusage.RateLimits{RateLimitResetCredits: &codexusage.RateLimitResetCreditsSummary{
			AvailableCount: 2,
			Credits:        []codexusage.RateLimitResetCredit{{ID: "reset-1", Status: "available", ExpiresAt: &expires}},
		}},
	}
	cache.Failures["profile-b"] = Failure{AttemptedAt: fetched, Message: "network unavailable", Attempts: 3}
	if err := Save(path, cache); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Profiles["profile-a"].PlanType != "pro" || !loaded.Profiles["profile-a"].FetchedAt.Equal(fetched) {
		t.Fatalf("unexpected cache round trip: %#v", loaded)
	}
	if failure := loaded.Failures["profile-b"]; failure.Message != "network unavailable" || failure.Attempts != 3 || !failure.AttemptedAt.Equal(fetched) {
		t.Fatalf("failure was not preserved: %#v", failure)
	}
	resetCredits := loaded.Profiles["profile-a"].RateLimits.RateLimitResetCredits
	if resetCredits == nil || resetCredits.AvailableCount != 2 || len(resetCredits.Credits) != 1 || resetCredits.Credits[0].ExpiresAt == nil || *resetCredits.Credits[0].ExpiresAt != expires {
		t.Fatalf("reset credits were not preserved: %#v", resetCredits)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("cache mode = %o", info.Mode().Perm())
		}
	}
}
