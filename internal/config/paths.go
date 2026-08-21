package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Paths struct {
	Root       string
	Vault      string
	State      string
	Journal    string
	UsageCache string
}

func ResolvePaths(override string) (Paths, error) {
	root := override
	if root == "" {
		root = os.Getenv("CODEX_SWITCH_HOME")
	}
	if root == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve user config directory: %w", err)
		}
		root = filepath.Join(base, "codex-switch")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Paths{}, fmt.Errorf("resolve codex-switch home: %w", err)
	}
	return Paths{
		Root:       abs,
		Vault:      filepath.Join(abs, "vault.v1.enc"),
		State:      filepath.Join(abs, "state.json"),
		Journal:    filepath.Join(abs, "switch.journal.json"),
		UsageCache: filepath.Join(abs, "usage-cache.v1.json"),
	}, nil
}

func (paths Paths) Ensure() error {
	if err := os.MkdirAll(paths.Root, 0o700); err != nil {
		return fmt.Errorf("create codex-switch home: %w", err)
	}
	return os.Chmod(paths.Root, 0o700)
}
