//go:build linux

package secretstore

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type linuxStore struct{}

func Open() (Store, error) {
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil, fmt.Errorf("secret-tool client for Secret Service is unavailable: %w", err)
	}
	return linuxStore{}, nil
}

func (linuxStore) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	command := exec.Command("secret-tool", "store", "--label", "codex-switch "+key, "service", "codex-switch", "target", target(key))
	command.Stdin = strings.NewReader(value)
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("write Secret Service entry: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (linuxStore) Get(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	command := exec.Command("secret-tool", "lookup", "service", "codex-switch", "target", target(key))
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

func (linuxStore) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	command := exec.Command("secret-tool", "clear", "service", "codex-switch", "target", target(key))
	if output, err := command.CombinedOutput(); err != nil {
		if strings.TrimSpace(string(output)) == "" {
			return ErrNotFound
		}
		return fmt.Errorf("delete Secret Service entry: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}
