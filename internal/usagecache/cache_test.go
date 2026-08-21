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
	if cache.Version != 1 || cache.Profiles == nil {
		t.Fatalf("unexpected empty cache: %#v", cache)
	}
	fetched := time.Date(2026, 8, 20, 1, 2, 3, 0, time.UTC)
	cache.Profiles["profile-a"] = codexusage.Snapshot{FetchedAt: fetched, PlanType: "pro"}
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
