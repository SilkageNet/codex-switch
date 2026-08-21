package codexlogin

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFindBinaryRejectsWindowsDesktopBundle(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows package paths are only rejected on Windows")
	}
	t.Setenv("CODEX_BINARY", "")

	path := `C:\Program Files\WindowsApps\OpenAI.Codex_26.818.3698.0_x64__2p2nqsd0c76g0\app\resources\codex.exe`
	found, err := FindBinary(path)
	if err == nil {
		t.Fatalf("FindBinary(%q) succeeded with %q", path, found)
	}
	if found != "" {
		t.Fatalf("FindBinary(%q) returned unusable path %q", path, found)
	}
	for _, expected := range []string{"cannot be launched externally on Windows", "standalone Codex CLI", "--codex-binary"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("error %q does not contain %q", err, expected)
		}
	}
}

func TestFindBinaryAcceptsExplicitStandaloneBinary(t *testing.T) {
	t.Setenv("CODEX_BINARY", "")

	path := `C:\Users\person\.local\bin\codex.exe`
	found, err := FindBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	if found != path {
		t.Fatalf("FindBinary(%q) = %q", path, found)
	}
}

func TestFindBinarySkipsWindowsDesktopBundleOnPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows package paths are only rejected on Windows")
	}
	t.Setenv("CODEX_BINARY", "")
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")

	root := t.TempDir()
	desktopDir := filepath.Join(root, "WindowsApps", "OpenAI.Codex_test_x64__package", "app", "resources")
	standaloneDir := filepath.Join(root, "standalone")
	for _, dir := range []string{desktopDir, standaloneDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "codex.exe"), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", strings.Join([]string{desktopDir, standaloneDir}, string(os.PathListSeparator)))

	found, err := FindBinary("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(standaloneDir, "codex.exe")
	if !strings.EqualFold(found, want) {
		t.Fatalf("FindBinary() = %q, want %q", found, want)
	}
}
