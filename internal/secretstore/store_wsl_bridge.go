//go:build linux || windows

package secretstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"unicode/utf16"
)

type powershellStore struct {
	binary string
}

type powershellRequest struct {
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Value     string `json:"value,omitempty"`
}

func (store powershellStore) Set(key, value string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	request := powershellRequest{
		Operation: "set",
		Target:    target(key),
		Value:     base64.StdEncoding.EncodeToString([]byte(value)),
	}
	_, err := store.run(request)
	return err
}

func (store powershellStore) Get(key string) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	output, err := store.run(powershellRequest{Operation: "get", Target: target(key)})
	if err != nil {
		return "", err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
	if err != nil {
		return "", errors.New("windows DPAPI returned an invalid credential payload")
	}
	return string(decoded), nil
}

func (store powershellStore) Delete(key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := store.run(powershellRequest{Operation: "delete", Target: target(key)})
	return err
}

func (store powershellStore) run(request powershellRequest) ([]byte, error) {
	input, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	command := exec.Command(
		store.binary,
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-EncodedCommand",
		encodePowerShell(wslPowerShellScript),
	)
	command.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 44 {
			return nil, ErrNotFound
		}
		message := fmt.Sprintf("run Windows DPAPI credential bridge for %s: %v", request.Operation, err)
		if strings.TrimSpace(stderr.String()) != "" {
			message += bridgeDiagnostic(stderr.String())
		}
		return nil, errors.New(message)
	}
	return stdout.Bytes(), nil
}

func bridgeDiagnostic(stderr string) string {
	const prefix = "CODEX_SWITCH_BRIDGE_ERROR:"
	start := strings.Index(stderr, prefix)
	if start < 0 {
		return " (PowerShell diagnostics redacted)"
	}
	fields := strings.SplitN(stderr[start+len(prefix):], ":", 3)
	if len(fields) < 2 || !safeDiagnosticToken(fields[0]) || !safeDiagnosticToken(fields[1]) {
		return " (PowerShell diagnostics redacted)"
	}
	return fmt.Sprintf(" at %s (%s)", fields[0], fields[1])
}

func safeDiagnosticToken(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func encodePowerShell(script string) string {
	encodedRunes := utf16.Encode([]rune(script))
	encodedBytes := make([]byte, len(encodedRunes)*2)
	for index, value := range encodedRunes {
		encodedBytes[index*2] = byte(value)
		encodedBytes[index*2+1] = byte(value >> 8)
	}
	return base64.StdEncoding.EncodeToString(encodedBytes)
}

const wslPowerShellScript = `$ErrorActionPreference = 'Stop'
$stage = 'read-request'
try {
    $request = [Console]::In.ReadToEnd() | ConvertFrom-Json
    $stage = 'hash-target'
    $targetBytes = [Text.Encoding]::UTF8.GetBytes([string]$request.target)
    $sha = [Security.Cryptography.SHA256]::Create()
    try {
        $property = -join ($sha.ComputeHash($targetBytes) | ForEach-Object { $_.ToString('x2') })
    } finally {
        $sha.Dispose()
    }
    $root = 'Software\SilkageNet\codex-switch\secrets'
    $entropy = [Text.Encoding]::UTF8.GetBytes('codex-switch:wsl-dpapi:v1')
    $stage = 'resolve-dpapi'
    $scope = [Security.Cryptography.DataProtectionScope]::CurrentUser

    switch ([string]$request.operation) {
        'set' {
            $stage = 'decode-value'
            $plain = [Convert]::FromBase64String([string]$request.value)
            try {
                $stage = 'protect-value'
                $cipher = [Security.Cryptography.ProtectedData]::Protect($plain, $entropy, $scope)
                $stage = 'open-registry-write'
                $registryKey = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($root)
                try {
                    $stage = 'write-registry'
                    $registryKey.SetValue($property, $cipher, [Microsoft.Win32.RegistryValueKind]::Binary)
                } finally {
                    if ($null -ne $registryKey) { $registryKey.Dispose() }
                }
            } finally {
                if ($null -ne $plain) { [Array]::Clear($plain, 0, $plain.Length) }
            }
        }
        'get' {
            $stage = 'open-registry-read'
            $registryKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($root, $false)
            if ($null -eq $registryKey) { exit 44 }
            try {
                $stage = 'read-registry'
                if ($registryKey.GetValueNames() -notcontains $property) { exit 44 }
                $cipher = [byte[]]$registryKey.GetValue($property)
            } finally {
                $registryKey.Dispose()
            }
            $stage = 'unprotect-value'
            $plain = [Security.Cryptography.ProtectedData]::Unprotect($cipher, $entropy, $scope)
            try {
                [Console]::Out.Write([Convert]::ToBase64String($plain))
            } finally {
                [Array]::Clear($plain, 0, $plain.Length)
            }
        }
        'delete' {
            $stage = 'open-registry-delete'
            $registryKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($root, $true)
            if ($null -eq $registryKey) { exit 44 }
            try {
                $stage = 'delete-registry'
                if ($registryKey.GetValueNames() -notcontains $property) { exit 44 }
                $registryKey.DeleteValue($property, $false)
            } finally {
                $registryKey.Dispose()
            }
        }
        default { throw 'unsupported credential operation' }
    }
} catch {
    [Console]::Error.Write(('CODEX_SWITCH_BRIDGE_ERROR:{0}:{1}' -f $stage, $_.Exception.GetType().Name))
    exit 1
}
`
