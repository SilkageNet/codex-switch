package vault

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SilkageNet/codex-switch/internal/secretstore"
)

func TestVaultRoundTripAndRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault.enc")
	secrets := secretstore.NewMemoryStore()
	manager := New(path, secrets)
	data, err := manager.Init()
	if err != nil {
		t.Fatal(err)
	}
	profile := NewProfile("work", "test", json.RawMessage(`{"auth_mode":"chatgpt"}`), "account-a", "", "person@example.com", data.UpdatedAt)
	if err := data.Add(profile, false); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(data); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("person@example.com")) || bytes.Contains(encoded, []byte("account-a")) {
		t.Fatal("vault leaked plaintext account metadata")
	}
	loaded, err := manager.Load()
	if err != nil || len(loaded.Profiles) != 1 || loaded.Profiles[0].Alias != "work" {
		t.Fatalf("unexpected loaded vault: %#v, %v", loaded, err)
	}
	if err := manager.RotateKey(); err != nil {
		t.Fatal(err)
	}
	rotated, err := manager.Load()
	if err != nil || len(rotated.Profiles) != 1 {
		t.Fatalf("rotated vault failed: %#v, %v", rotated, err)
	}
}

func TestPortableExportImport(t *testing.T) {
	data := Data{Version: 1, Profiles: []Profile{{ID: "id", Alias: "personal", AccountID: "account"}}}
	encoded, err := Export(data, []byte("a long test passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("personal")) {
		t.Fatal("portable backup leaked plaintext")
	}
	loaded, err := Import(encoded, []byte("a long test passphrase"))
	if err != nil || len(loaded.Profiles) != 1 || loaded.Profiles[0].Alias != "personal" {
		t.Fatalf("unexpected import: %#v, %v", loaded, err)
	}
	if _, err := Import(encoded, []byte("wrong passphrase")); err == nil {
		t.Fatal("wrong passphrase should fail")
	}
}

func TestDuplicateAlias(t *testing.T) {
	data := Data{Version: 1}
	if err := data.Add(Profile{Alias: "Work"}, false); err != nil {
		t.Fatal(err)
	}
	if err := data.Add(Profile{Alias: "work"}, false); err == nil {
		t.Fatal("aliases must be case-insensitively unique")
	}
}

func TestValidateAlias(t *testing.T) {
	for _, alias := range []string{"", " work", "work\n"} {
		if err := ValidateAlias(alias); err == nil {
			t.Fatalf("alias %q should be rejected", alias)
		}
	}
	if err := ValidateAlias("work-account"); err != nil {
		t.Fatal(err)
	}
}
