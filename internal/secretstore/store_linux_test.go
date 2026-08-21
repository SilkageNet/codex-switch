//go:build linux

package secretstore

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestOpenUsesWindowsDPAPIOnWSLWithoutSecretTool(t *testing.T) {
	directory := t.TempDir()
	powershell := filepath.Join(directory, "powershell.exe")
	if err := os.WriteFile(powershell, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")

	store, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	bridge, ok := store.(wslStore)
	if !ok {
		t.Fatalf("expected WSL store, got %T", store)
	}
	if bridge.fallback != nil {
		t.Fatalf("unexpected Secret Service fallback: %T", bridge.fallback)
	}
	primary, ok := bridge.primary.(powershellStore)
	if !ok || primary.binary != powershell {
		t.Fatalf("unexpected WSL primary store: %#v", bridge.primary)
	}
}

func TestPowerShellStoreKeepsSecretOutOfArguments(t *testing.T) {
	store, argumentsPath, inputPath := testPowerShellStore(t)
	t.Setenv("CODEX_SWITCH_BRIDGE_MODE", "set")
	if err := store.Set("master-key/test", "generated-vault-key"); err != nil {
		t.Fatal(err)
	}
	arguments, err := os.ReadFile(argumentsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(arguments), "generated-vault-key") {
		t.Fatal("secret was exposed in process arguments")
	}
	input, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(input), "generated-vault-key") {
		t.Fatal("secret was not encoded in the stdin request")
	}
	var request powershellRequest
	if err := json.Unmarshal(input, &request); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(request.Value)
	if err != nil || string(decoded) != "generated-vault-key" {
		t.Fatalf("unexpected bridge request: %#v, %v", request, err)
	}
}

func TestPowerShellStoreGetAndNotFound(t *testing.T) {
	store, _, _ := testPowerShellStore(t)
	t.Setenv("CODEX_SWITCH_BRIDGE_MODE", "get")
	value, err := store.Get("master-key/test")
	if err != nil {
		t.Fatal(err)
	}
	if value != "stored-vault-key" {
		t.Fatalf("unexpected stored value %q", value)
	}

	t.Setenv("CODEX_SWITCH_BRIDGE_MODE", "missing")
	if err := store.Delete("master-key/test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestPowerShellStoreRedactsDiagnostics(t *testing.T) {
	store, _, _ := testPowerShellStore(t)
	t.Setenv("CODEX_SWITCH_BRIDGE_MODE", "fail")
	_, err := store.Get("master-key/test")
	if err == nil || strings.Contains(err.Error(), "sensitive diagnostic") || !strings.Contains(err.Error(), "redacted") {
		t.Fatalf("unexpected bridge error: %v", err)
	}
}

func TestPowerShellStoreAllowsOnlySanitizedDiagnostics(t *testing.T) {
	store, _, _ := testPowerShellStore(t)
	t.Setenv("CODEX_SWITCH_BRIDGE_MODE", "safe-fail")
	_, err := store.Get("master-key/test")
	if err == nil || !strings.Contains(err.Error(), "at protect-value (TypeLoadException)") {
		t.Fatalf("unexpected bridge error: %v", err)
	}

	t.Setenv("CODEX_SWITCH_BRIDGE_MODE", "unsafe-fail")
	_, err = store.Get("master-key/test")
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.Contains(err.Error(), "redacted") {
		t.Fatalf("unexpected unsafe bridge error: %v", err)
	}
}

func TestWSLStoreFallsBackToSecretService(t *testing.T) {
	fallback := NewMemoryStore()
	if err := fallback.Set("master-key/test", "legacy-value"); err != nil {
		t.Fatal(err)
	}
	store := wslStore{primary: errorStore{err: ErrNotFound}, fallback: fallback}
	value, err := store.Get("master-key/test")
	if err != nil || value != "legacy-value" {
		t.Fatalf("fallback read failed: %q, %v", value, err)
	}
}

func TestEncodePowerShellUsesUTF16LE(t *testing.T) {
	encoded, err := base64.StdEncoding.DecodeString(encodePowerShell("A中"))
	if err != nil {
		t.Fatal(err)
	}
	units := make([]uint16, len(encoded)/2)
	for index := range units {
		units[index] = uint16(encoded[index*2]) | uint16(encoded[index*2+1])<<8
	}
	if decoded := string(utf16.Decode(units)); decoded != "A中" {
		t.Fatalf("unexpected encoded command %q", decoded)
	}
}

type errorStore struct {
	err error
}

func (store errorStore) Set(string, string) error { return store.err }

func (store errorStore) Get(string) (string, error) { return "", store.err }

func (store errorStore) Delete(string) error { return store.err }

func testPowerShellStore(t *testing.T) (powershellStore, string, string) {
	t.Helper()
	directory := t.TempDir()
	argumentsPath := filepath.Join(directory, "arguments")
	inputPath := filepath.Join(directory, "input")
	powershell := filepath.Join(directory, "powershell.exe")
	stub := `#!/bin/sh
printf '%s\n' "$@" > "$CODEX_SWITCH_ARGUMENTS"
cat > "$CODEX_SWITCH_INPUT"
case "$CODEX_SWITCH_BRIDGE_MODE" in
  get) printf 'c3RvcmVkLXZhdWx0LWtleQ==' ;;
  missing) exit 44 ;;
  fail) printf 'sensitive diagnostic' >&2; exit 1 ;;
  safe-fail) printf 'CODEX_SWITCH_BRIDGE_ERROR:protect-value:TypeLoadException\nCLIXML wrapper' >&2; exit 1 ;;
  unsafe-fail) printf 'CODEX_SWITCH_BRIDGE_ERROR:protect-value:secret value' >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(powershell, []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_SWITCH_ARGUMENTS", argumentsPath)
	t.Setenv("CODEX_SWITCH_INPUT", inputPath)
	return powershellStore{binary: powershell}, argumentsPath, inputPath
}
