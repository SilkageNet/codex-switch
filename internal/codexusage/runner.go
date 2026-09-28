package codexusage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
	"github.com/SilkageNet/codex-switch/internal/authschema"
)

const maxProtocolMessage = 8 << 20

type Runner struct {
	Binary        string
	BinaryError   error
	ClientVersion string
	command       func(context.Context, string, ...string) *exec.Cmd
}

type rpcRequest struct {
	Method string `json:"method"`
	ID     *int   `json:"id,omitempty"`
	Params any    `json:"params,omitempty"`
}

type rpcResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (runner Runner) Query(ctx context.Context, auth json.RawMessage) (Snapshot, json.RawMessage, error) {
	if runner.BinaryError != nil {
		return Snapshot{}, nil, runner.BinaryError
	}
	if runner.Binary == "" {
		return Snapshot{}, nil, errors.New("official Codex executable not found; install Codex or pass --codex-binary")
	}
	document, err := authschema.Parse(auth)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("validate saved account: %w", err)
	}
	temporaryHome, err := os.MkdirTemp("", "codex-switch-usage-*")
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("create temporary CODEX_HOME: %w", err)
	}
	defer func() { _ = os.RemoveAll(temporaryHome) }()
	if err := os.Chmod(temporaryHome, 0o700); err != nil {
		return Snapshot{}, nil, fmt.Errorf("protect temporary CODEX_HOME: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(temporaryHome, "config.toml"), []byte("cli_auth_credentials_store = \"file\"\n"), 0o600); err != nil {
		return Snapshot{}, nil, err
	}
	if err := atomicfile.Write(filepath.Join(temporaryHome, "auth.json"), append(append([]byte(nil), document.Raw...), '\n'), 0o600); err != nil {
		return Snapshot{}, nil, err
	}

	commandFactory := runner.command
	if commandFactory == nil {
		commandFactory = exec.CommandContext
	}
	command := commandFactory(ctx, runner.Binary, "app-server")
	environment := command.Env
	if environment == nil {
		environment = os.Environ()
	}
	command.Env = withEnvironment(environment, "CODEX_HOME", temporaryHome)
	stdin, err := command.StdinPipe()
	if err != nil {
		return Snapshot{}, nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Snapshot{}, nil, err
	}
	var stderr limitedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return Snapshot{}, nil, fmt.Errorf("start Codex app server: %w", err)
	}
	waited := false
	defer func() {
		_ = stdin.Close()
		if !waited && command.Process != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()

	encoder := json.NewEncoder(stdin)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), maxProtocolMessage)
	version := runner.ClientVersion
	if version == "" {
		version = "dev"
	}
	if err := sendRequest(encoder, 0, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "codex_switch", "title": "codex-switch", "version": version},
	}); err != nil {
		return Snapshot{}, nil, err
	}
	initialize, err := readResponse(scanner, 0)
	if err != nil {
		return Snapshot{}, nil, protocolError("initialize", err, stderr.String())
	}
	if initialize.Error != nil {
		return Snapshot{}, nil, responseError("initialize", initialize.Error)
	}
	if err := encoder.Encode(rpcRequest{Method: "initialized", Params: map[string]any{}}); err != nil {
		return Snapshot{}, nil, fmt.Errorf("send initialized notification: %w", err)
	}
	if err := sendRequest(encoder, 1, "account/read", map[string]bool{"refreshToken": false}); err != nil {
		return Snapshot{}, nil, err
	}
	accountResponse, err := readResponse(scanner, 1)
	if err != nil {
		return Snapshot{}, nil, protocolError("read account", err, stderr.String())
	}
	if accountResponse.Error != nil {
		return Snapshot{}, nil, responseError("account/read", accountResponse.Error)
	}
	var account AccountResponse
	if err := json.Unmarshal(accountResponse.Result, &account); err != nil {
		return Snapshot{}, nil, fmt.Errorf("decode account/read response: %w", err)
	}
	if account.Account == nil || account.Account.Type != "chatgpt" {
		return Snapshot{}, nil, errors.New("saved profile is not recognized as a ChatGPT account by Codex")
	}

	if err := sendRequest(encoder, 2, "account/rateLimits/read", nil); err != nil {
		return Snapshot{}, nil, err
	}
	rateLimitsResponse, err := readResponse(scanner, 2)
	if err != nil {
		return Snapshot{}, nil, protocolError("read account rate limits", err, stderr.String())
	}
	if err := sendRequest(encoder, 3, "account/usage/read", nil); err != nil {
		return Snapshot{}, nil, err
	}
	tokenUsageResponse, err := readResponse(scanner, 3)
	if err != nil {
		return Snapshot{}, nil, protocolError("read account token usage", err, stderr.String())
	}
	_ = stdin.Close()
	if err := command.Wait(); err != nil {
		waited = true
		if ctx.Err() != nil {
			return Snapshot{}, nil, fmt.Errorf("query account usage: %w", ctx.Err())
		}
		return Snapshot{}, nil, protocolError("Codex app server exited", err, stderr.String())
	}
	waited = true
	updated, err := atomicfile.ReadLimited(filepath.Join(temporaryHome, "auth.json"), 2<<20)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("read refreshed account credentials: %w", err)
	}
	updatedDocument, err := authschema.Parse(updated)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("validate refreshed account credentials: %w", err)
	}
	if updatedDocument.Tokens.AccountID != document.Tokens.AccountID {
		return Snapshot{}, nil, errors.New("codex returned credentials for a different account")
	}

	snapshot := Snapshot{FetchedAt: time.Now().UTC(), PlanType: account.Account.PlanType}
	if rateLimitsResponse.Error != nil {
		snapshot.Partial = append(snapshot.Partial, "rateLimits")
	} else {
		var value RateLimits
		if err := json.Unmarshal(rateLimitsResponse.Result, &value); err != nil {
			return Snapshot{}, nil, fmt.Errorf("decode account/rateLimits/read response: %w", err)
		}
		snapshot.RateLimits = &value
	}
	if tokenUsageResponse.Error != nil {
		snapshot.Partial = append(snapshot.Partial, "tokenUsage")
	} else {
		var value TokenUsage
		if err := json.Unmarshal(tokenUsageResponse.Result, &value); err != nil {
			return Snapshot{}, nil, fmt.Errorf("decode account/usage/read response: %w", err)
		}
		snapshot.TokenUsage = &value
	}
	if snapshot.RateLimits == nil && snapshot.TokenUsage == nil {
		return Snapshot{}, updatedDocument.Raw, fmt.Errorf("account usage requests failed: %w", errors.Join(
			responseError("account/rateLimits/read", rateLimitsResponse.Error),
			responseError("account/usage/read", tokenUsageResponse.Error),
		))
	}
	return snapshot, updatedDocument.Raw, nil
}

func sendRequest(encoder *json.Encoder, id int, method string, params any) error {
	if err := encoder.Encode(rpcRequest{Method: method, ID: &id, Params: params}); err != nil {
		return fmt.Errorf("send %s request: %w", method, err)
	}
	return nil
}

func readResponse(scanner *bufio.Scanner, expected int) (rpcResponse, error) {
	responses, err := readResponses(scanner, expected)
	return responses[expected], err
}

func readResponses(scanner *bufio.Scanner, expected ...int) (map[int]rpcResponse, error) {
	wanted := make(map[int]bool, len(expected))
	for _, id := range expected {
		wanted[id] = true
	}
	responses := make(map[int]rpcResponse, len(expected))
	for len(responses) < len(wanted) && scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			return nil, fmt.Errorf("decode JSON-RPC message: %w", err)
		}
		if len(response.ID) == 0 || bytes.Equal(response.ID, []byte("null")) {
			continue
		}
		id, err := strconv.Atoi(strings.Trim(string(response.ID), `"`))
		if err != nil || !wanted[id] {
			continue
		}
		responses[id] = response
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(responses) != len(wanted) {
		return nil, io.ErrUnexpectedEOF
	}
	return responses, nil
}

func responseError(method string, rpcErr *rpcError) error {
	return fmt.Errorf("%s failed (%d): %s", method, rpcErr.Code, rpcErr.Message)
}

func protocolError(operation string, err error, stderr string) error {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %w (Codex app-server diagnostics redacted)", operation, err)
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

type limitedBuffer struct {
	buffer bytes.Buffer
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	const limit = 16 << 10
	original := len(data)
	remaining := limit - buffer.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
		}
		_, _ = buffer.buffer.Write(data)
	}
	return original, nil
}

func (buffer *limitedBuffer) String() string {
	return buffer.buffer.String()
}
