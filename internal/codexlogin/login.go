package codexlogin

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
	"github.com/SilkageNet/codex-switch/internal/authschema"
)

type Runner struct {
	Binary string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func FindBinary(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if configured := os.Getenv("CODEX_BINARY"); configured != "" {
		return configured, nil
	}
	if path, err := exec.LookPath("codex"); err == nil {
		return path, nil
	}
	if path := "/Applications/ChatGPT.app/Contents/Resources/codex"; fileExists(path) {
		return path, nil
	}
	return "", fmt.Errorf("codex executable not found; install Codex CLI or set CODEX_BINARY")
}

func (runner Runner) Login(deviceAuth bool) (authschema.Document, error) {
	temporaryHome, err := os.MkdirTemp("", "codex-switch-login-*")
	if err != nil {
		return authschema.Document{}, fmt.Errorf("create temporary CODEX_HOME: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporaryHome) }()

	config := []byte("cli_auth_credentials_store = \"file\"\n")
	if err := atomicfile.Write(filepath.Join(temporaryHome, "config.toml"), config, 0o600); err != nil {
		return authschema.Document{}, err
	}
	args := []string{"login"}
	if deviceAuth {
		args = append(args, "--device-auth")
	}
	command := exec.Command(runner.Binary, args...)
	command.Env = withEnvironment(os.Environ(), "CODEX_HOME", temporaryHome)
	command.Stdin = runner.Stdin
	command.Stdout = runner.Stdout
	command.Stderr = runner.Stderr
	if err := command.Run(); err != nil {
		return authschema.Document{}, fmt.Errorf("official codex login failed: %w", err)
	}
	data, err := atomicfile.ReadLimited(filepath.Join(temporaryHome, "auth.json"), 2<<20)
	if err != nil {
		return authschema.Document{}, fmt.Errorf("read login result: %w", err)
	}
	document, err := authschema.Parse(data)
	if err != nil {
		return authschema.Document{}, fmt.Errorf("validate login result: %w", err)
	}
	return document, nil
}

func (runner Runner) Version() (string, error) {
	output, err := exec.Command(runner.Binary, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("run codex --version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func withEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
