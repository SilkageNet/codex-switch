package switcher

import (
	"encoding/json"
	"errors"
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

func TestObserveUsesLiveAccountInsteadOfRecordedState(t *testing.T) {
	service, manager, data := testService(t)
	kun := profileFor(t, "kun", "account-kun", "refresh-kun", "2026-08-20T00:00:00Z")
	silkage := profileFor(t, "silkage", "account-silkage", "refresh-silkage", "2026-08-20T00:00:00Z")
	if err := data.Add(kun, false); err != nil {
		t.Fatal(err)
	}
	if err := data.Add(silkage, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	savedKun, _ := data.Find("kun")
	savedSilkage, _ := data.Find("silkage")
	if err := service.Home.WriteAuth(savedSilkage.Auth); err != nil {
		t.Fatal(err)
	}
	if err := appstate.Save(service.Paths.State, appstate.State{ActiveProfileID: savedKun.ID, AuthHash: "stale"}); err != nil {
		t.Fatal(err)
	}

	observation, err := service.Observe()
	if err != nil {
		t.Fatal(err)
	}
	if observation.ProfileID != savedSilkage.ID || observation.Alias != "silkage" || observation.State != AccountStateExternalLogin {
		t.Fatalf("unexpected observation: %#v", observation)
	}
	if observation.RecordedProfileID != savedKun.ID || observation.RecordedAlias != "kun" || !observation.NeedsSync {
		t.Fatalf("stale recorded state was not reported: %#v", observation)
	}
	state, err := appstate.Load(service.Paths.State)
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveProfileID != savedKun.ID {
		t.Fatal("read-only observation modified recorded state")
	}
}

func TestSyncRepairsExternalLoginAndAdoptsNewerCredentials(t *testing.T) {
	service, manager, data := testService(t)
	kun := profileFor(t, "kun", "account-kun", "refresh-kun", "2026-08-20T00:00:00Z")
	silkage := profileFor(t, "silkage", "account-silkage", "refresh-old", "2026-08-20T00:00:00Z")
	if err := data.Add(kun, false); err != nil {
		t.Fatal(err)
	}
	if err := data.Add(silkage, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	savedKun, _ := data.Find("kun")
	savedSilkage, _ := data.Find("silkage")
	live := authBytes("account-silkage", "refresh-new", "2026-08-20T01:00:00Z")
	if err := service.Home.WriteAuth(live); err != nil {
		t.Fatal(err)
	}
	if err := appstate.Save(service.Paths.State, appstate.State{ActiveProfileID: savedKun.ID, AuthHash: "stale"}); err != nil {
		t.Fatal(err)
	}

	result, err := service.Sync(SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || !result.CredentialsUpdated || !result.StateRepaired || result.ProfileID != savedSilkage.ID {
		t.Fatalf("unexpected sync result: %#v", result)
	}
	loaded, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := loaded.Find("silkage")
	document, _ := authschema.Parse(updated.Auth)
	if document.Tokens.RefreshToken != "refresh-new" {
		t.Fatal("newer live credentials were not saved")
	}
	state, err := appstate.Load(service.Paths.State)
	if err != nil {
		t.Fatal(err)
	}
	liveHash, _ := service.Home.AuthHash()
	if state.ActiveProfileID != savedSilkage.ID || state.AuthHash != liveHash {
		t.Fatalf("active state was not repaired: %#v", state)
	}
}

func TestSyncRequiresExplicitPreferenceForAmbiguousCredentials(t *testing.T) {
	service, manager, data := testService(t)
	profile := profileFor(t, "silkage", "account-silkage", "refresh-old", "2026-08-20T00:00:00Z")
	if err := data.Add(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	live := authBytes("account-silkage", "refresh-new", "2026-08-20T00:00:00Z")
	if err := service.Home.WriteAuth(live); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(SyncOptions{}); err == nil {
		t.Fatal("expected ambiguous credential protection")
	}
	loaded, _ := manager.Load()
	saved, _ := loaded.Find("silkage")
	savedDocument, _ := authschema.Parse(saved.Auth)
	if savedDocument.Tokens.RefreshToken != "refresh-old" {
		t.Fatal("ambiguous credentials were overwritten without consent")
	}

	result, err := service.Sync(SyncOptions{PreferLive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.CredentialsUpdated {
		t.Fatalf("live credentials were not adopted: %#v", result)
	}
}

func TestSyncImportsUnmanagedLiveLogin(t *testing.T) {
	service, manager, _ := testService(t)
	if err := service.Home.WriteAuth(authBytes("account-new", "refresh-new", "2026-08-20T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(SyncOptions{}); err == nil {
		t.Fatal("expected unmanaged login protection")
	}
	result, err := service.Sync(SyncOptions{Alias: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Imported || !result.CredentialsUpdated || result.Alias != "new" {
		t.Fatalf("unexpected import result: %#v", result)
	}
	loaded, _ := manager.Load()
	if _, err := loaded.Find("new"); err != nil {
		t.Fatal(err)
	}
}

func TestUseRepairsExternallyActivatedTargetWithoutProjection(t *testing.T) {
	service, manager, data := testService(t)
	kun := profileFor(t, "kun", "account-kun", "refresh-kun", "2026-08-20T00:00:00Z")
	silkage := profileFor(t, "silkage", "account-silkage", "refresh-silkage", "2026-08-20T00:00:00Z")
	if err := data.Add(kun, false); err != nil {
		t.Fatal(err)
	}
	if err := data.Add(silkage, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	savedKun, _ := data.Find("kun")
	savedSilkage, _ := data.Find("silkage")
	if err := service.Home.WriteAuth(savedSilkage.Auth); err != nil {
		t.Fatal(err)
	}
	if err := appstate.Save(service.Paths.State, appstate.State{ActiveProfileID: savedKun.ID, AuthHash: "stale"}); err != nil {
		t.Fatal(err)
	}

	result, err := service.Use("silkage", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || !result.StateRepaired {
		t.Fatalf("unexpected use result: %#v", result)
	}
	live, _ := service.Home.ReadAuth()
	document, _ := authschema.Parse(live)
	if document.Tokens.AccountID != "account-silkage" || document.Tokens.RefreshToken != "refresh-silkage" {
		t.Fatal("already-active credentials were re-projected")
	}
}

func TestObserveRejectsDuplicateIdentityWithoutExactCredentialMatch(t *testing.T) {
	service, manager, data := testService(t)
	for _, alias := range []string{"first", "second"} {
		if err := data.Add(profileFor(t, alias, "account-a", "refresh-"+alias, "2026-08-20T00:00:00Z"), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	if err := service.Home.WriteAuth(authBytes("account-a", "refresh-live", "2026-08-20T01:00:00Z")); err != nil {
		t.Fatal(err)
	}
	observation, err := service.Observe()
	if err != nil {
		t.Fatal(err)
	}
	if observation.State != AccountStateAmbiguous || observation.Managed {
		t.Fatalf("duplicate identity was not treated as ambiguous: %#v", observation)
	}
	if _, err := service.Sync(SyncOptions{}); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected ambiguous sync error: %v", err)
	}
}

func TestIdentifyCurrentUsesWorkspaceWhenAccountHasMultipleProfiles(t *testing.T) {
	_, _, data := testService(t)
	first := profileFor(t, "first", "account-a", "refresh-first", "2026-08-20T00:00:00Z")
	first.WorkspaceID = "workspace-a"
	second := profileFor(t, "second", "account-a", "refresh-second", "2026-08-20T00:00:00Z")
	second.WorkspaceID = "workspace-b"
	if err := data.Add(first, false); err != nil {
		t.Fatal(err)
	}
	if err := data.Add(second, false); err != nil {
		t.Fatal(err)
	}
	live, err := authschema.Parse(authBytes("account-a", "refresh-live", "2026-08-20T01:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	live.WorkspaceID = "workspace-b"
	profile, err := identifyCurrent(&data, live)
	if err != nil {
		t.Fatal(err)
	}
	if profile == nil || profile.Alias != "second" {
		t.Fatalf("workspace did not disambiguate account profiles: %#v", profile)
	}
}

func TestSyncRequiresPreferenceBeforeReplacingNewerSavedCredentials(t *testing.T) {
	service, manager, data := testService(t)
	profile := profileFor(t, "silkage", "account-silkage", "refresh-newer", "2026-08-20T01:00:00Z")
	if err := data.Add(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	if err := service.Home.WriteAuth(authBytes("account-silkage", "refresh-live", "2026-08-20T00:00:00Z")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(SyncOptions{}); err == nil {
		t.Fatal("expected newer saved credential protection")
	}
	loaded, _ := manager.Load()
	saved, _ := loaded.Find("silkage")
	savedDocument, _ := authschema.Parse(saved.Auth)
	if savedDocument.Tokens.RefreshToken != "refresh-newer" {
		t.Fatal("newer saved credentials were replaced without consent")
	}
	result, err := service.Sync(SyncOptions{PreferLive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.CredentialsUpdated {
		t.Fatalf("explicit live preference was not applied: %#v", result)
	}
}

func TestSyncClearsRecordedAccountAfterExternalLogout(t *testing.T) {
	service, manager, data := testService(t)
	profile := profileFor(t, "silkage", "account-silkage", "refresh", "2026-08-20T00:00:00Z")
	if err := data.Add(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	saved, _ := data.Find("silkage")
	if err := appstate.Save(service.Paths.State, appstate.State{ActiveProfileID: saved.ID, AuthHash: "old"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.Sync(SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !result.StateRepaired || result.DetectedState != AccountStateLoggedOut {
		t.Fatalf("unexpected logout sync result: %#v", result)
	}
	state, _ := appstate.Load(service.Paths.State)
	if state.ActiveProfileID != "" || state.AuthHash != "" {
		t.Fatalf("stale state was not cleared: %#v", state)
	}
}

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
