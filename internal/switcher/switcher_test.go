package switcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SilkageNet/codex-switch/internal/authschema"
	"github.com/SilkageNet/codex-switch/internal/codexhome"
	appconfig "github.com/SilkageNet/codex-switch/internal/config"
	"github.com/SilkageNet/codex-switch/internal/secretstore"
	appstate "github.com/SilkageNet/codex-switch/internal/state"
	"github.com/SilkageNet/codex-switch/internal/vault"
)

func TestUseAdoptsRotatedLiveTokenAndSwitches(t *testing.T) {
	service, manager, data := testService(t)
	oldTime := "2026-08-20T00:00:00Z"
	newTime := "2026-08-20T01:00:00Z"
	profileA := profileFor(t, "a", "account-a", "refresh-a0", oldTime)
	profileB := profileFor(t, "b", "account-b", "refresh-b0", oldTime)
	if err := data.Add(profileA, false); err != nil {
		t.Fatal(err)
	}
	if err := data.Add(profileB, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	saved, _ := data.Find("a")
	liveA := authBytes("account-a", "refresh-a1", newTime)
	if err := service.Home.WriteAuth(liveA); err != nil {
		t.Fatal(err)
	}
	hash, _ := service.Home.AuthHash()
	if err := appstate.Save(service.Paths.State, appstate.State{ActiveProfileID: saved.ID, AuthHash: hash}); err != nil {
		t.Fatal(err)
	}

	result, err := service.Use("b", true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Alias != "b" {
		t.Fatalf("unexpected switch result: %#v", result)
	}
	live, err := service.Home.ReadAuth()
	if err != nil {
		t.Fatal(err)
	}
	document, err := authschema.Parse(live)
	if err != nil || document.Tokens.AccountID != "account-b" {
		t.Fatalf("unexpected active auth: %#v, %v", document.Public(), err)
	}
	loaded, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	updatedA, _ := loaded.Find("a")
	updatedDocument, _ := authschema.Parse(updatedA.Auth)
	if updatedDocument.Tokens.RefreshToken != "refresh-a1" {
		t.Fatalf("rotated refresh token was not adopted")
	}
}

func TestUseSameProfileKeepsRotatedLiveToken(t *testing.T) {
	service, manager, data := testService(t)
	profile := profileFor(t, "a", "account-a", "refresh-a0", "2026-08-20T00:00:00Z")
	if err := data.Add(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	saved, _ := data.Find("a")
	live := authBytes("account-a", "refresh-a1", "2026-08-20T01:00:00Z")
	if err := service.Home.WriteAuth(live); err != nil {
		t.Fatal(err)
	}
	hash, _ := service.Home.AuthHash()
	if err := appstate.Save(service.Paths.State, appstate.State{ActiveProfileID: saved.ID, AuthHash: hash}); err != nil {
		t.Fatal(err)
	}

	result, err := service.Use("a", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("same profile should not be re-projected after reconciliation")
	}
	active, err := service.Home.ReadAuth()
	if err != nil {
		t.Fatal(err)
	}
	document, err := authschema.Parse(active)
	if err != nil {
		t.Fatal(err)
	}
	if document.Tokens.RefreshToken != "refresh-a1" {
		t.Fatal("rotated live token was overwritten")
	}
	loaded, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := loaded.Find("a")
	updatedDocument, _ := authschema.Parse(updated.Auth)
	if updatedDocument.Tokens.RefreshToken != "refresh-a1" {
		t.Fatal("rotated live token was not persisted")
	}
}

func TestUseRejectsUnmanagedLiveLogin(t *testing.T) {
	service, manager, data := testService(t)
	if err := data.Add(profileFor(t, "b", "account-b", "refresh-b", "2026-08-20T00:00:00Z"), false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	if err := service.Home.WriteAuth(authBytes("unmanaged", "refresh-x", "2026-08-20T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Use("b", true); err == nil {
		t.Fatal("expected unmanaged login protection")
	}
}

func TestRecoverCommittedSwitch(t *testing.T) {
	service, manager, data := testService(t)
	profile := profileFor(t, "b", "account-b", "refresh-b", "2026-08-20T00:00:00Z")
	if err := data.Add(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	saved, _ := data.Find("b")
	if err := service.Home.WriteAuth(saved.Auth); err != nil {
		t.Fatal(err)
	}
	newHash, _ := service.Home.AuthHash()
	if err := service.writeJournal(Journal{Version: 1, Operation: "use", TargetProfileID: saved.ID, OldHash: "missing", NewHash: newHash, PreparedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := service.Recover(); err != nil {
		t.Fatal(err)
	}
	state, err := appstate.Load(service.Paths.State)
	if err != nil || state.ActiveProfileID != saved.ID {
		t.Fatalf("recovery did not commit state: %#v, %v", state, err)
	}
	if _, err := os.Stat(service.Paths.Journal); !os.IsNotExist(err) {
		t.Fatal("journal was not removed")
	}
}

func testService(t *testing.T) (Service, *vault.Manager, vault.Data) {
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
	return Service{Home: home, Paths: paths, Vault: manager}, manager, data
}

func profileFor(t *testing.T, alias, account, refresh, lastRefresh string) vault.Profile {
	t.Helper()
	document, err := authschema.Parse(authBytes(account, refresh, lastRefresh))
	if err != nil {
		t.Fatal(err)
	}
	updatedAt, _ := document.GenerationTime()
	return vault.NewProfile(alias, "test", document.Raw, account, "", alias+"@example.com", updatedAt)
}

func authBytes(account, refresh, lastRefresh string) []byte {
	value := map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"id_token":      "id-" + refresh,
			"access_token":  "access-" + refresh,
			"refresh_token": refresh,
			"account_id":    account,
		},
		"last_refresh": lastRefresh,
	}
	encoded, _ := json.Marshal(value)
	return encoded
}
