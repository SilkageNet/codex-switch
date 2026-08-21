package codexusage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerQueriesUsageAndReturnsRefreshedCredentials(t *testing.T) {
	runner := helperRunner(t, false)
	snapshot, updated, err := runner.Query(context.Background(), testAuth("account-a", "refresh-old", "2026-08-20T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PlanType != "pro" || snapshot.RateLimits == nil || snapshot.TokenUsage == nil {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if snapshot.MainRateLimit() == nil || snapshot.MainRateLimit().Primary.UsedPercent != 21 {
		t.Fatalf("unexpected main rate limit: %#v", snapshot.MainRateLimit())
	}
	if snapshot.TokenUsage.Summary.LifetimeTokens == nil || *snapshot.TokenUsage.Summary.LifetimeTokens != 1234567 {
		t.Fatalf("unexpected token usage: %#v", snapshot.TokenUsage)
	}
	var wire struct {
		Tokens struct {
			Refresh string `json:"refresh_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(updated, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Tokens.Refresh != "refresh-new" {
		t.Fatalf("refreshed credentials were not returned: %s", wire.Tokens.Refresh)
	}
}

func TestRunnerAllowsOneUnsupportedUsageMethod(t *testing.T) {
	runner := helperRunner(t, true)
	snapshot, _, err := runner.Query(context.Background(), testAuth("account-a", "refresh-old", "2026-08-20T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.RateLimits == nil || snapshot.TokenUsage != nil || len(snapshot.Partial) != 1 || snapshot.Partial[0] != "tokenUsage" {
		t.Fatalf("unexpected partial snapshot: %#v", snapshot)
	}
}

func TestProtocolErrorRedactsChildDiagnostics(t *testing.T) {
	err := protocolError("query", context.DeadlineExceeded, "secret child output")
	if strings.Contains(err.Error(), "secret child output") || !strings.Contains(err.Error(), "redacted") {
		t.Fatalf("unexpected protocol error: %v", err)
	}
}

func helperRunner(t *testing.T, partial bool) Runner {
	t.Helper()
	return Runner{
		Binary:        os.Args[0],
		ClientVersion: "test",
		command: func(ctx context.Context, binary string, args ...string) *exec.Cmd {
			command := exec.CommandContext(ctx, binary, "-test.run=TestCodexUsageHelperProcess", "--")
			command.Env = append(os.Environ(), "CODEX_USAGE_HELPER=1")
			if partial {
				command.Env = append(command.Env, "CODEX_USAGE_PARTIAL=1")
			}
			return command
		},
	}
}

func TestCodexUsageHelperProcess(t *testing.T) {
	if os.Getenv("CODEX_USAGE_HELPER") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || len(request.ID) == 0 {
			continue
		}
		id := string(request.ID)
		switch request.Method {
		case "initialize":
			_ = encoder.Encode(map[string]any{"id": json.RawMessage(id), "result": map[string]any{}})
		case "account/read":
			_ = encoder.Encode(map[string]any{"id": json.RawMessage(id), "result": map[string]any{
				"account":            map[string]any{"type": "chatgpt", "email": "a@example.com", "planType": "pro"},
				"requiresOpenaiAuth": true,
			}})
		case "account/rateLimits/read":
			_ = encoder.Encode(map[string]any{"id": json.RawMessage(id), "result": map[string]any{
				"rateLimits":          map[string]any{"planType": "pro", "primary": map[string]any{"usedPercent": 21, "windowDurationMins": 300}},
				"rateLimitsByLimitId": map[string]any{"codex": map[string]any{"planType": "pro", "primary": map[string]any{"usedPercent": 21, "windowDurationMins": 300}}},
			}})
		case "account/usage/read":
			if os.Getenv("CODEX_USAGE_PARTIAL") == "1" {
				_ = encoder.Encode(map[string]any{"id": json.RawMessage(id), "error": map[string]any{"code": -32601, "message": "method not found"}})
			} else {
				lifetime := int64(1234567)
				_ = encoder.Encode(map[string]any{"id": json.RawMessage(id), "result": map[string]any{
					"summary":           map[string]any{"lifetimeTokens": lifetime},
					"dailyUsageBuckets": []map[string]any{{"startDate": "2026-08-20", "tokens": 99}},
				}})
			}
			home := os.Getenv("CODEX_HOME")
			_ = os.WriteFile(filepath.Join(home, "auth.json"), testAuth("account-a", "refresh-new", "2026-08-20T01:00:00Z"), 0o600)
		}
	}
	os.Exit(0)
}

func testAuth(account, refresh, lastRefresh string) []byte {
	return []byte(fmt.Sprintf(`{"auth_mode":"chatgpt","tokens":{"id_token":%q,"access_token":%q,"refresh_token":%q,"account_id":%q},"last_refresh":%q}`, "id-"+refresh, "access-"+refresh, refresh, account, lastRefresh))
}
