//go:build darwin

package secretstore

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const serviceName = "codex-switch"

type darwinStore struct{}

func Open() (Store, error) {
	if _, err := exec.LookPath("security"); err != nil {
		return nil, fmt.Errorf("macOS Keychain command is unavailable: %w", err)
	}
	return darwinStore{}, nil
}

func (darwinStore) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	command := exec.Command("security", "add-generic-password", "-U", "-a", target(key), "-s", serviceName, "-w")
	command.Stdin = strings.NewReader(value + "\n")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("write macOS Keychain entry: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (darwinStore) Get(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	command := exec.Command("security", "find-generic-password", "-a", target(key), "-s", serviceName, "-w")
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if isDarwinNotFound(stderr.String(), err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("read macOS Keychain entry: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

func isDarwinNotFound(stderr string, err error) bool {
	if strings.Contains(stderr, "SecKeychainSearchCreateFromAttributes") {
		return false
	}
	if strings.Contains(stderr, "could not be found") {
		return true
	}
	var exitError *exec.ExitError
	return strings.TrimSpace(stderr) == "" && errors.As(err, &exitError) && exitError.ExitCode() == 44
}

func (darwinStore) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	command := exec.Command("security", "delete-generic-password", "-a", target(key), "-s", serviceName)
	if output, err := command.CombinedOutput(); err != nil {
		if isDarwinNotFound(string(output), err) {
			return ErrNotFound
		}
		return fmt.Errorf("delete macOS Keychain entry: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}
