package projectstate

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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
	"github.com/SilkageNet/codex-switch/internal/codexhome"
	"github.com/SilkageNet/codex-switch/internal/filelock"
	"github.com/SilkageNet/codex-switch/internal/process"
)

const (
	globalStateName    = ".codex-global-state.json"
	maxGlobalStateSize = 32 << 20
	maxProtocolMessage = 8 << 20
)

type Inspection struct {
	Available                 bool   `json:"available"`
	HostKey                   string `json:"hostKey,omitempty"`
	Projects                  int    `json:"projects"`
	LegacyAssignments         int    `json:"legacyAssignments"`
	ResolvableAssignments     int    `json:"resolvableAssignments"`
	PendingAssignments        int    `json:"pendingAssignments"`
	ProjectsMigrated          bool   `json:"projectsMigrated"`
	ThreadAssignmentsMigrated bool   `json:"threadAssignmentsMigrated"`
	NeedsRepair               bool   `json:"needsRepair"`
}

type Result struct {
	Inspection
	Repaired            bool   `json:"repaired"`
	AssignmentsRepaired int    `json:"assignmentsRepaired"`
	Backup              string `json:"backup,omitempty"`
}

type Service struct {
	Home          codexhome.Home
	Binary        string
	BinaryError   error
	ClientVersion string
	Runner        *Runner
}

type assignment struct {
	ProjectKind string `json:"projectKind"`
	ProjectID   string `json:"projectId"`
}

type migration struct {
	Version                    int      `json:"version"`
	ProjectsMigrated           bool     `json:"projectsMigrated"`
	ThreadAssignmentsMigrated  bool     `json:"threadAssignmentsMigrated"`
	PendingThreadAssignmentIDs []string `json:"pendingThreadAssignmentIds"`
}

type document struct {
	raw          map[string]json.RawMessage
	originalHash string
	statePath    string
	hostKey      string
	projects     map[string]json.RawMessage
	assignments  map[string]assignment
	projectIDs   map[string]string
	migrations   map[string]migration
	inspection   Inspection
	resolved     []resolvedAssignment
}

type resolvedAssignment struct {
	ThreadID  string
	ProjectID string
}

func (service Service) Inspect() (Inspection, error) {
	document, err := load(service.Home.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Inspection{}, nil
		}
		return Inspection{}, err
	}
	return document.inspection, nil
}

func (service Service) Repair(ctx context.Context, allowRunning, force bool) (Result, error) {
	lock, err := filelock.Acquire(filepath.Join(service.Home.Path, ".codex-switch.lock"))
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = lock.Close() }()

	document, err := load(service.Home.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Result{}, nil
		}
		return Result{}, err
	}
	result := Result{Inspection: document.inspection}
	if !force && !document.inspection.NeedsRepair {
		return result, nil
	}
	if len(document.resolved) == 0 {
		return result, errors.New("no resolvable local project assignments were found")
	}
	if !allowRunning {
		running, detectErr := process.DetectCodex()
		if detectErr != nil {
			return result, fmt.Errorf("detect running Codex processes: %w", detectErr)
		}
		if len(running) > 0 {
			return result, fmt.Errorf("codex is still running (PID %d, %s); close it before repairing project assignments or explicitly pass --allow-running", running[0].PID, running[0].Command)
		}
	}
	if service.BinaryError != nil {
		return result, service.BinaryError
	}
	if service.Binary == "" {
		return result, errors.New("official Codex executable not found; install Codex or pass --codex-binary")
	}

	runner := service.Runner
	if runner == nil {
		runner = &Runner{Binary: service.Binary, ClientVersion: service.ClientVersion}
	}
	if err := runner.Assign(ctx, service.Home.Path, document.resolved); err != nil {
		return result, err
	}
	backup, err := document.markMigrated()
	if err != nil {
		return result, fmt.Errorf("project assignments were repaired but the migration checkpoint could not be saved: %w", err)
	}
	result.Repaired = true
	result.AssignmentsRepaired = len(document.resolved)
	result.Backup = backup
	result.ThreadAssignmentsMigrated = true
	result.PendingAssignments = 0
	result.NeedsRepair = false
	return result, nil
}

func load(home string) (document, error) {
	statePath := filepath.Join(home, globalStateName)
	data, err := atomicfile.ReadLimited(statePath, maxGlobalStateSize)
	if err != nil {
		return document{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return document{}, fmt.Errorf("decode %s: %w", statePath, err)
	}
	var projects map[string]json.RawMessage
	if value := raw["local-projects"]; len(value) > 0 {
		if err := json.Unmarshal(value, &projects); err != nil {
			return document{}, fmt.Errorf("decode local projects: %w", err)
		}
	}
	var assignments map[string]assignment
	if value := raw["thread-project-assignments"]; len(value) > 0 {
		if err := json.Unmarshal(value, &assignments); err != nil {
			return document{}, fmt.Errorf("decode thread project assignments: %w", err)
		}
	}
	var projectIDsByHost map[string]map[string]string
	if value := raw["app-server-project-id-by-legacy-project-id-by-host"]; len(value) > 0 {
		if err := json.Unmarshal(value, &projectIDsByHost); err != nil {
			return document{}, fmt.Errorf("decode project ID mappings: %w", err)
		}
	}
	var migrations map[string]migration
	if value := raw["app-server-projects-migration-by-host"]; len(value) > 0 {
		if err := json.Unmarshal(value, &migrations); err != nil {
			return document{}, fmt.Errorf("decode project migration state: %w", err)
		}
	}
	hostKey := "local:" + home
	projectIDs := projectIDsByHost[hostKey]
	migrationState, available := migrations[hostKey]
	resolved := make([]resolvedAssignment, 0, len(assignments))
	for threadID, legacy := range assignments {
		if legacy.ProjectKind != "local" || strings.TrimSpace(threadID) == "" {
			continue
		}
		projectID := projectIDs[legacy.ProjectID]
		if projectID == "" {
			continue
		}
		resolved = append(resolved, resolvedAssignment{ThreadID: threadID, ProjectID: projectID})
	}
	sort.Slice(resolved, func(first, second int) bool {
		return resolved[first].ThreadID < resolved[second].ThreadID
	})
	inspection := Inspection{
		Available:                 available,
		HostKey:                   hostKey,
		Projects:                  len(projects),
		LegacyAssignments:         len(assignments),
		ResolvableAssignments:     len(resolved),
		PendingAssignments:        len(migrationState.PendingThreadAssignmentIDs),
		ProjectsMigrated:          migrationState.ProjectsMigrated,
		ThreadAssignmentsMigrated: migrationState.ThreadAssignmentsMigrated,
	}
	inspection.NeedsRepair = available && migrationState.ProjectsMigrated && !migrationState.ThreadAssignmentsMigrated && len(resolved) > 0
	return document{
		raw:          raw,
		originalHash: atomicfile.Hash(data),
		statePath:    statePath,
		hostKey:      hostKey,
		projects:     projects,
		assignments:  assignments,
		projectIDs:   projectIDs,
		migrations:   migrations,
		inspection:   inspection,
		resolved:     resolved,
	}, nil
}

func (document *document) markMigrated() (string, error) {
	migrationState, ok := document.migrations[document.hostKey]
	if !ok {
		return "", errors.New("local project migration checkpoint disappeared")
	}
	migrationState.ThreadAssignmentsMigrated = true
	migrationState.PendingThreadAssignmentIDs = []string{}
	document.migrations[document.hostKey] = migrationState
	encodedMigrations, err := json.Marshal(document.migrations)
	if err != nil {
		return "", err
	}
	document.raw["app-server-projects-migration-by-host"] = encodedMigrations
	output, err := json.Marshal(document.raw)
	if err != nil {
		return "", err
	}
	output = append(output, '\n')
	original, err := atomicfile.ReadLimited(document.statePath, maxGlobalStateSize)
	if err != nil {
		return "", err
	}
	if atomicfile.Hash(original) != document.originalHash {
		return "", errors.New("codex global state changed during project repair; the assignment updates are safe, but the migration checkpoint was not advanced; close Codex and retry")
	}
	backup := fmt.Sprintf("%s.codex-switch.bak.%s", document.statePath, time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := atomicfile.Write(backup, original, 0o600); err != nil {
		return "", fmt.Errorf("back up global state: %w", err)
	}
	if err := atomicfile.Write(document.statePath, output, 0o600); err != nil {
		return "", fmt.Errorf("save global state: %w", err)
	}
	return backup, nil
}

type Runner struct {
	Binary        string
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

type projectListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
	NextCursor *string `json:"nextCursor"`
}

type threadUpdateResponse struct {
	Thread struct {
		ID        string  `json:"id"`
		ProjectID *string `json:"projectId"`
	} `json:"thread"`
}

func (runner Runner) Assign(ctx context.Context, home string, assignments []resolvedAssignment) error {
	commandFactory := runner.command
	if commandFactory == nil {
		commandFactory = exec.CommandContext
	}
	command := commandFactory(ctx, runner.Binary, "app-server")
	environment := command.Env
	if environment == nil {
		environment = os.Environ()
	}
	command.Env = replaceEnvironment(environment, "CODEX_HOME", home)
	stdin, err := command.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr limitedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return fmt.Errorf("start Codex app server: %w", err)
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
		"clientInfo":   map[string]string{"name": "codex_switch", "title": "codex-switch", "version": version},
		"capabilities": map[string]bool{"experimentalApi": true},
	}); err != nil {
		return err
	}
	response, err := readResponse(scanner, 0)
	if err != nil {
		return protocolError("initialize", err, stderr.String())
	}
	if response.Error != nil {
		return responseError("initialize", response.Error)
	}
	if err := encoder.Encode(rpcRequest{Method: "initialized", Params: map[string]any{}}); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}

	projects := map[string]bool{}
	nextID := 1
	var cursor *string
	for {
		params := map[string]any{"limit": 100}
		if cursor != nil {
			params["cursor"] = *cursor
		}
		if err := sendRequest(encoder, nextID, "project/list", params); err != nil {
			return err
		}
		response, err = readResponse(scanner, nextID)
		if err != nil {
			return protocolError("list projects", err, stderr.String())
		}
		if response.Error != nil {
			return responseError("project/list", response.Error)
		}
		var page projectListResponse
		if err := json.Unmarshal(response.Result, &page); err != nil {
			return fmt.Errorf("decode project/list response: %w", err)
		}
		for _, project := range page.Data {
			projects[project.ID] = true
		}
		nextID++
		cursor = page.NextCursor
		if cursor == nil || *cursor == "" {
			break
		}
	}
	for _, item := range assignments {
		if !projects[item.ProjectID] {
			return fmt.Errorf("mapped project %s for thread %s is not present in Codex", item.ProjectID, item.ThreadID)
		}
	}
	for index, item := range assignments {
		requestID := nextID + index
		if err := sendRequest(encoder, requestID, "thread/metadata/update", map[string]string{
			"threadId":  item.ThreadID,
			"projectId": item.ProjectID,
		}); err != nil {
			return err
		}
		response, err = readResponse(scanner, requestID)
		if err != nil {
			return protocolError("update thread project", err, stderr.String())
		}
		if response.Error != nil {
			return fmt.Errorf("repair project assignment for thread %s: %w", item.ThreadID, responseError("thread/metadata/update", response.Error))
		}
		var updated threadUpdateResponse
		if err := json.Unmarshal(response.Result, &updated); err != nil {
			return fmt.Errorf("decode thread/metadata/update response: %w", err)
		}
		if updated.Thread.ID != item.ThreadID || updated.Thread.ProjectID == nil || *updated.Thread.ProjectID != item.ProjectID {
			return fmt.Errorf("codex did not confirm project assignment for thread %s", item.ThreadID)
		}
	}
	if err := stdin.Close(); err != nil {
		return err
	}
	if err := command.Wait(); err != nil {
		waited = true
		if ctx.Err() != nil {
			return fmt.Errorf("repair Codex projects: %w", ctx.Err())
		}
		return protocolError("Codex app server exited", err, stderr.String())
	}
	waited = true
	return nil
}

func sendRequest(encoder *json.Encoder, id int, method string, params any) error {
	if err := encoder.Encode(rpcRequest{Method: method, ID: &id, Params: params}); err != nil {
		return fmt.Errorf("send %s request: %w", method, err)
	}
	return nil
}

func readResponse(scanner *bufio.Scanner, expected int) (rpcResponse, error) {
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			return rpcResponse{}, fmt.Errorf("decode JSON-RPC message: %w", err)
		}
		if len(response.ID) == 0 || bytes.Equal(response.ID, []byte("null")) {
			continue
		}
		id, err := strconv.Atoi(strings.Trim(string(response.ID), `"`))
		if err == nil && id == expected {
			return response, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return rpcResponse{}, err
	}
	return rpcResponse{}, io.EOF
}

func responseError(method string, rpcErr *rpcError) error {
	if rpcErr == nil {
		return nil
	}
	return fmt.Errorf("%s failed (%d): %s", method, rpcErr.Code, rpcErr.Message)
}

func protocolError(operation string, err error, stderr string) error {
	diagnostic := strings.TrimSpace(stderr)
	if diagnostic == "" {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return fmt.Errorf("%s: %w (Codex app-server diagnostics redacted)", operation, err)
}

func replaceEnvironment(environment []string, key, value string) []string {
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
	data []byte
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	const limit = 64 << 10
	remaining := limit - len(buffer.data)
	if remaining > 0 {
		if remaining > len(data) {
			remaining = len(data)
		}
		buffer.data = append(buffer.data, data[:remaining]...)
	}
	return len(data), nil
}

func (buffer *limitedBuffer) String() string {
	return string(buffer.data)
}
