package projectstate

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
	"github.com/SilkageNet/codex-switch/internal/codexhome"
)

func TestInspectAndRepairProjectAssignments(t *testing.T) {
	homePath := t.TempDir()
	writeGlobalState(t, homePath)
	service := Service{
		Home:          codexhome.Home{Path: homePath},
		Binary:        "codex",
		ClientVersion: "test",
		Runner: &Runner{Binary: "codex", command: func(context.Context, string, ...string) *exec.Cmd {
			command := exec.Command(os.Args[0], "-test.run=TestProjectStateHelperProcess", "--")
			command.Env = append(os.Environ(), "GO_WANT_PROJECT_STATE_HELPER=1")
			return command
		}},
	}

	inspection, err := service.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.NeedsRepair || inspection.Projects != 1 || inspection.ResolvableAssignments != 2 || inspection.PendingAssignments != 1 {
		t.Fatalf("unexpected inspection: %#v", inspection)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := service.Repair(ctx, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Repaired || result.AssignmentsRepaired != 2 || result.Backup == "" {
		t.Fatalf("unexpected repair result: %#v", result)
	}
	if _, err := os.Stat(result.Backup); err != nil {
		t.Fatalf("backup was not created: %v", err)
	}

	after, err := service.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if after.NeedsRepair || !after.ThreadAssignmentsMigrated || after.PendingAssignments != 0 {
		t.Fatalf("migration checkpoint was not updated: %#v", after)
	}
	data, err := atomicfile.ReadLimited(filepath.Join(homePath, globalStateName), maxGlobalStateSize)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["unrelated-setting"]) != `{"kept":true}` {
		t.Fatalf("unrelated state was not preserved: %s", raw["unrelated-setting"])
	}
}

func TestRepairIsNoopAfterMigration(t *testing.T) {
	homePath := t.TempDir()
	writeGlobalState(t, homePath)
	statePath := filepath.Join(homePath, globalStateName)
	document, err := load(homePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := document.markMigrated(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (Service{Home: codexhome.Home{Path: homePath}}).Repair(context.Background(), true, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Repaired {
		t.Fatalf("unexpected repair: %#v", result)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("no-op repair changed global state")
	}
}

func TestProjectStateHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_PROJECT_STATE_HELPER") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			Method string          `json:"method"`
			ID     *int            `json:"id"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(2)
		}
		if request.ID == nil {
			continue
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"userAgent": "test"}
		case "project/list":
			result = map[string]any{"data": []map[string]string{{"id": "server-project"}}, "nextCursor": nil}
		case "thread/metadata/update":
			var params struct {
				ThreadID  string `json:"threadId"`
				ProjectID string `json:"projectId"`
			}
			if err := json.Unmarshal(request.Params, &params); err != nil {
				os.Exit(3)
			}
			result = map[string]any{"thread": map[string]string{"id": params.ThreadID, "projectId": params.ProjectID}}
		default:
			_ = encoder.Encode(map[string]any{"id": *request.ID, "error": map[string]any{"code": -32601, "message": "unknown method"}})
			continue
		}
		if err := encoder.Encode(map[string]any{"id": *request.ID, "result": result}); err != nil {
			os.Exit(4)
		}
	}
	os.Exit(0)
}

func writeGlobalState(t *testing.T, homePath string) {
	t.Helper()
	hostKey := "local:" + homePath
	state := map[string]any{
		"local-projects": map[string]any{
			"legacy-project": map[string]any{"id": "legacy-project", "name": "Daily", "rootPaths": []string{homePath}},
		},
		"thread-project-assignments": map[string]any{
			"thread-a": map[string]string{"projectKind": "local", "projectId": "legacy-project"},
			"thread-b": map[string]string{"projectKind": "local", "projectId": "legacy-project"},
		},
		"app-server-project-id-by-legacy-project-id-by-host": map[string]any{
			hostKey: map[string]string{"legacy-project": "server-project"},
		},
		"app-server-projects-migration-by-host": map[string]any{
			hostKey: map[string]any{
				"version":                    1,
				"projectsMigrated":           true,
				"threadAssignmentsMigrated":  false,
				"pendingThreadAssignmentIds": []string{"thread-a"},
			},
		},
		"unrelated-setting": map[string]bool{"kept": true},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicfile.Write(filepath.Join(homePath, globalStateName), append(data, '\n'), 0o600); err != nil {
		t.Fatal(fmt.Errorf("write test global state: %w", err))
	}
}
