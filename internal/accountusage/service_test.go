package accountusage

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/SilkageNet/codex-switch/internal/authschema"
	"github.com/SilkageNet/codex-switch/internal/codexhome"
	"github.com/SilkageNet/codex-switch/internal/codexusage"
	appconfig "github.com/SilkageNet/codex-switch/internal/config"
	"github.com/SilkageNet/codex-switch/internal/secretstore"
	appstate "github.com/SilkageNet/codex-switch/internal/state"
	"github.com/SilkageNet/codex-switch/internal/vault"
)

type fakeRunner struct {
	snapshot codexusage.Snapshot
	auth     json.RawMessage
	err      error
}

func (runner fakeRunner) Query(context.Context, json.RawMessage) (codexusage.Snapshot, json.RawMessage, error) {
	return runner.snapshot, runner.auth, runner.err
}

func TestRefreshCachesUsageAndAdoptsRotatedCredentials(t *testing.T) {
	service, manager, profile := testService(t, false)
	updated := authBytes("account-a", "refresh-new", "2026-08-20T01:00:00Z")
	lifetime := int64(1234)
	service.Runner = fakeRunner{
		snapshot: codexusage.Snapshot{FetchedAt: time.Now().UTC(), PlanType: "pro", TokenUsage: &codexusage.TokenUsage{Summary: codexusage.TokenUsageSummary{LifetimeTokens: &lifetime}}},
		auth:     updated,
	}
	results, err := service.Refresh(context.Background(), []string{profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if results[profile.ID].Error != "" {
		t.Fatalf("unexpected refresh warning: %s", results[profile.ID].Error)
	}
	loaded, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := loaded.Find(profile.ID)
	document, _ := authschema.Parse(saved.Auth)
	if document.Tokens.RefreshToken != "refresh-new" {
		t.Fatal("rotated credential was not saved")
	}
	cache, err := service.Cached()
	if err != nil || cache.Profiles[profile.ID].PlanType != "pro" {
		t.Fatalf("usage was not cached: %#v, %v", cache, err)
	}
}

func TestRefreshProjectsRotatedCredentialsForActiveProfile(t *testing.T) {
	service, manager, profile := testService(t, true)
	service.Runner = fakeRunner{
		snapshot: codexusage.Snapshot{FetchedAt: time.Now().UTC(), PlanType: "plus"},
		auth:     authBytes("account-a", "refresh-new", "2026-08-20T01:00:00Z"),
	}
	results, err := service.Refresh(context.Background(), []string{profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if results[profile.ID].Error != "" {
		t.Fatalf("unexpected refresh warning: %s", results[profile.ID].Error)
	}
	live, err := service.Home.ReadAuth()
	if err != nil {
		t.Fatal(err)
	}
	liveDocument, _ := authschema.Parse(live)
	if liveDocument.Tokens.RefreshToken != "refresh-new" {
		t.Fatal("active credential projection was not refreshed")
	}
	loaded, _ := manager.Load()
	saved, _ := loaded.Find(profile.ID)
	savedDocument, _ := authschema.Parse(saved.Auth)
	if savedDocument.Tokens.RefreshToken != "refresh-new" {
		t.Fatal("active vault credential was not refreshed")
	}
}

func TestRefreshRejectsCredentialsForDifferentAccount(t *testing.T) {
	service, manager, profile := testService(t, false)
	service.Runner = fakeRunner{
		snapshot: codexusage.Snapshot{FetchedAt: time.Now().UTC(), PlanType: "pro"},
		auth:     authBytes("account-other", "refresh-new", "2026-08-20T01:00:00Z"),
	}
	results, err := service.Refresh(context.Background(), []string{profile.ID})
	if err != nil {
		t.Fatal(err)
	}
	if results[profile.ID].Error == "" {
		t.Fatal("expected credential identity warning")
	}
	loaded, _ := manager.Load()
	saved, _ := loaded.Find(profile.ID)
	document, _ := authschema.Parse(saved.Auth)
	if document.Tokens.RefreshToken != "refresh-old" {
		t.Fatal("mismatched credential was saved")
	}
	cache, cacheErr := service.Cached()
	if cacheErr != nil {
		t.Fatal(cacheErr)
	}
	if _, ok := cache.Profiles[profile.ID]; ok {
		t.Fatal("usage for mismatched credentials was cached")
	}
}

func testService(t *testing.T, active bool) (Service, *vault.Manager, vault.Profile) {
	t.Helper()
	root := t.TempDir()
	home, err := codexhome.Resolve(filepath.Join(root, "codex"))
	if err != nil || home.Ensure() != nil {
		t.Fatal(err)
	}
	paths, err := appconfig.ResolvePaths(filepath.Join(root, "switch"))
	if err != nil || paths.Ensure() != nil {
		t.Fatal(err)
	}
	manager := vault.New(paths.Vault, secretstore.NewMemoryStore())
	data, err := manager.Init()
	if err != nil {
		t.Fatal(err)
	}
	document, err := authschema.Parse(authBytes("account-a", "refresh-old", "2026-08-20T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	updatedAt, _ := document.GenerationTime()
	profile := vault.NewProfile("a", "test", document.Raw, "account-a", "", "a@example.com", updatedAt)
	if err := data.Add(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	saved, _ := data.Find("a")
	if active {
		if err := home.WriteAuth(saved.Auth); err != nil {
			t.Fatal(err)
		}
		hash, _ := home.AuthHash()
		if err := appstate.Save(paths.State, appstate.State{ActiveProfileID: saved.ID, AuthHash: hash}); err != nil {
			t.Fatal(err)
		}
	}
	return Service{Home: home, Paths: paths, Vault: manager}, manager, *saved
}

func authBytes(account, refresh, lastRefresh string) []byte {
	return []byte(fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q,"access_token":%q,"refresh_token":%q,"account_id":%q},"last_refresh":%q}`, "id-"+refresh, "access-"+refresh, refresh, account, lastRefresh))
}
