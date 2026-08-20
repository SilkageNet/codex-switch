package codexhome

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
)

const (
	MaxAuthBytes   int64 = 2 << 20
	MaxConfigBytes int64 = 4 << 20
)

type Home struct {
	Path       string
	AuthPath   string
	ConfigPath string
}

func Resolve(override string) (Home, error) {
	root := override
	if root == "" {
		root = os.Getenv("CODEX_HOME")
	}
	if root == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return Home{}, fmt.Errorf("resolve user home: %w", err)
		}
		root = filepath.Join(userHome, ".codex")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Home{}, fmt.Errorf("resolve CODEX_HOME: %w", err)
	}
	if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(abs)
		if resolveErr != nil {
			return Home{}, fmt.Errorf("resolve CODEX_HOME symlink: %w", resolveErr)
		}
		abs = resolved
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Home{}, fmt.Errorf("inspect CODEX_HOME: %w", err)
	}
	return Home{
		Path:       abs,
		AuthPath:   filepath.Join(abs, "auth.json"),
		ConfigPath: filepath.Join(abs, "config.toml"),
	}, nil
}

func (home Home) Ensure() error {
	if err := os.MkdirAll(home.Path, 0o700); err != nil {
		return fmt.Errorf("create CODEX_HOME: %w", err)
	}
	return nil
}

func (home Home) ReadAuth() ([]byte, error) {
	return atomicfile.ReadLimited(home.AuthPath, MaxAuthBytes)
}

func (home Home) AuthHash() (string, error) {
	return atomicfile.FileHash(home.AuthPath)
}

func (home Home) WriteAuth(data []byte) error {
	if int64(len(data)) > MaxAuthBytes {
		return fmt.Errorf("authentication document exceeds %d bytes", MaxAuthBytes)
	}
	output := append([]byte(nil), data...)
	output = append(output, '\n')
	return atomicfile.Write(home.AuthPath, output, 0o600)
}

func (home Home) DeleteAuth() error {
	info, err := os.Lstat(home.AuthPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to delete symlink %s", home.AuthPath)
	}
	return os.Remove(home.AuthPath)
}

type CredentialStoreMode string

const (
	StoreFile    CredentialStoreMode = "file"
	StoreKeyring CredentialStoreMode = "keyring"
	StoreAuto    CredentialStoreMode = "auto"
)

var storeLine = regexp.MustCompile(`^\s*cli_auth_credentials_store\s*=\s*["'](file|keyring|auto)["']\s*(?:#.*)?$`)

func (home Home) CredentialStore() (CredentialStoreMode, error) {
	data, err := atomicfile.ReadLimited(home.ConfigPath, MaxConfigBytes)
	if errors.Is(err, os.ErrNotExist) {
		return StoreAuto, nil
	}
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		match := storeLine.FindStringSubmatch(line)
		if len(match) == 2 {
			return CredentialStoreMode(match[1]), nil
		}
	}
	return StoreAuto, nil
}

func (home Home) EnableFileStore() (string, error) {
	if err := home.Ensure(); err != nil {
		return "", err
	}
	data, err := atomicfile.ReadLimited(home.ConfigPath, MaxConfigBytes)
	if errors.Is(err, os.ErrNotExist) {
		data = nil
	} else if err != nil {
		return "", err
	}

	backup := ""
	if len(data) > 0 {
		backup = fmt.Sprintf("%s.codex-switch.bak.%s", home.ConfigPath, time.Now().UTC().Format("20060102T150405Z"))
		if err := atomicfile.Write(backup, data, 0o600); err != nil {
			return "", fmt.Errorf("back up config.toml: %w", err)
		}
	}

	lines := strings.Split(string(data), "\n")
	replaced := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		if storeLine.MatchString(line) {
			lines[index] = `cli_auth_credentials_store = "file"`
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append([]string{`cli_auth_credentials_store = "file"`, ""}, lines...)
	}
	output := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	if err := atomicfile.Write(home.ConfigPath, []byte(output), 0o600); err != nil {
		return backup, fmt.Errorf("enable file credential store: %w", err)
	}
	return backup, nil
}
