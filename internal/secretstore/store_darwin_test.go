//go:build darwin

package secretstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDarwinSetSuppliesPasswordToWFlag(t *testing.T) {
	directory := t.TempDir()
	security := filepath.Join(directory, "security")
	stub := `#!/bin/sh
test "$#" -eq 8 || exit 64
test "$1" = "add-generic-password" || exit 65
test "$2" = "-U" || exit 66
test "$3" = "-a" || exit 67
test "$4" = "codex-switch/master-key/test" || exit 68
test "$5" = "-s" || exit 69
test "$6" = "codex-switch" || exit 70
test "$7" = "-w" || exit 71
test "$8" = "generated-vault-key" || exit 72
`
	if err := os.WriteFile(security, []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	if err := (darwinStore{}).Set("master-key/test", "generated-vault-key"); err != nil {
		t.Fatal(err)
	}
}
