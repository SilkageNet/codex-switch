//go:build linux

package secretstore

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type linuxStore struct {
	binary string
}

type wslStore struct {
	primary  Store
	fallback Store
}

func Open() (Store, error) {
	secretTool, secretToolErr := exec.LookPath("secret-tool")
	if isWSL() {
		powershell, powershellErr := findWindowsPowerShell()
		if powershellErr == nil {
			store := wslStore{primary: powershellStore{binary: powershell}}
			if secretToolErr == nil {
				store.fallback = linuxStore{binary: secretTool}
			}
			return store, nil
		}
		if secretToolErr == nil {
			return linuxStore{binary: secretTool}, nil
		}
		return nil, fmt.Errorf("WSL was detected, but neither Windows PowerShell nor secret-tool is available: %w", powershellErr)
	}
	if secretToolErr != nil {
		return nil, fmt.Errorf("secret-tool client for Secret Service is unavailable; install the libsecret command-line tools: %w", secretToolErr)
	}
	return linuxStore{binary: secretTool}, nil
}

func (store linuxStore) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	command := exec.Command(store.binary, "store", "--label", "codex-switch "+key, "service", "codex-switch", "target", target(key))
	command.Stdin = strings.NewReader(value)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("write Secret Service entry: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (store linuxStore) Get(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	command := exec.Command(store.binary, "lookup", "service", "codex-switch", "target", target(key))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if strings.TrimSpace(stderr.String()) == "" {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("read Secret Service entry: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	if len(output) == 0 {
		return "", ErrNotFound
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

func (store linuxStore) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	command := exec.Command(store.binary, "clear", "service", "codex-switch", "target", target(key))
	if output, err := command.CombinedOutput(); err != nil {
		if strings.TrimSpace(string(output)) == "" {
			return ErrNotFound
		}
		return fmt.Errorf("delete Secret Service entry: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (store wslStore) Set(key, value string) error {
	primaryErr := store.primary.Set(key, value)
	if primaryErr == nil || store.fallback == nil {
		return primaryErr
	}
	fallbackErr := store.fallback.Set(key, value)
	if fallbackErr == nil {
		return nil
	}
	return fmt.Errorf("write WSL credential store: %w", errors.Join(primaryErr, fallbackErr))
}

func (store wslStore) Get(key string) (string, error) {
	value, primaryErr := store.primary.Get(key)
	if primaryErr == nil || store.fallback == nil {
		return value, primaryErr
	}
	value, fallbackErr := store.fallback.Get(key)
	if fallbackErr == nil {
		return value, nil
	}
	if errors.Is(primaryErr, ErrNotFound) && errors.Is(fallbackErr, ErrNotFound) {
		return "", ErrNotFound
	}
	return "", fmt.Errorf("read WSL credential store: %w", errors.Join(primaryErr, fallbackErr))
}

func (store wslStore) Delete(key string) error {
	primaryErr := store.primary.Delete(key)
	if store.fallback == nil {
		return primaryErr
	}
	fallbackErr := store.fallback.Delete(key)
	if primaryErr == nil || fallbackErr == nil {
		return nil
	}
	if errors.Is(primaryErr, ErrNotFound) && errors.Is(fallbackErr, ErrNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("delete WSL credential store: %w", errors.Join(primaryErr, fallbackErr))
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	for _, path := range []string{"/proc/sys/kernel/osrelease", "/proc/version"} {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft") {
			return true
		}
	}
	return false
}

func findWindowsPowerShell() (string, error) {
	for _, name := range []string{"powershell.exe", "pwsh.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	for _, path := range []string{
		"/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe",
		"/mnt/c/Program Files/PowerShell/7/pwsh.exe",
	} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return filepath.Clean(path), nil
		}
	}
	return "", errors.New("windows PowerShell executable was not found; enable WSL interoperability and Windows drive mounting")
}
