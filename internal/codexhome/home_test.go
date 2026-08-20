package codexhome

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableFileStorePreservesConfig(t *testing.T) {
	root := t.TempDir()
	home, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	original := "model = \"gpt-test\"\n\n[mcp_servers.demo]\ncommand = \"demo\"\n"
	if err := os.WriteFile(home.ConfigPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := home.EnableFileStore()
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected a backup")
	}
	data, err := os.ReadFile(home.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "cli_auth_credentials_store = \"file\"\n") || !strings.Contains(text, original) {
		t.Fatalf("config was not preserved:\n%s", text)
	}
	mode, err := home.CredentialStore()
	if err != nil || mode != StoreFile {
		t.Fatalf("unexpected store %q, %v", mode, err)
	}
	if _, err := os.Stat(filepath.Clean(backup)); err != nil {
		t.Fatal(err)
	}
}

func TestCredentialStoreStopsAtFirstTable(t *testing.T) {
	root := t.TempDir()
	home, _ := Resolve(root)
	if err := os.WriteFile(home.ConfigPath, []byte("[profile.demo]\ncli_auth_credentials_store = \"file\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mode, err := home.CredentialStore()
	if err != nil || mode != StoreAuto {
		t.Fatalf("profile-scoped value must not be treated as top-level: %q, %v", mode, err)
	}
}
