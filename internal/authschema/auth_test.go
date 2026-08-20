package authschema

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestParseAndMetadata(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{
		"email":                       "person@example.com",
		"https://api.openai.com/auth": map[string]any{"workspace_id": "ws-test"},
	})
	idToken := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	document, err := Parse(testAuth("account-a", "refresh-a", "2026-08-20T00:00:00Z", idToken))
	if err != nil {
		t.Fatal(err)
	}
	if document.Email != "person@example.com" || document.WorkspaceID != "ws-test" {
		t.Fatalf("unexpected metadata: %#v", document.Public())
	}
}

func TestParseRejectsMissingRefreshToken(t *testing.T) {
	_, err := Parse([]byte(`{"auth_mode":"chatgpt","tokens":{"account_id":"a","access_token":"b"}}`))
	if err == nil {
		t.Fatal("expected missing refresh token to fail")
	}
}

func TestCompareGeneration(t *testing.T) {
	saved, _ := Parse(testAuth("account-a", "refresh-1", "2026-08-20T00:00:00Z", "id-1"))
	live, _ := Parse(testAuth("account-a", "refresh-2", "2026-08-20T01:00:00Z", "id-2"))
	decision, err := CompareGeneration(saved, live)
	if err != nil || decision != GenerationAdoptLive {
		t.Fatalf("expected live adoption, got %v, %v", decision, err)
	}

	ambiguous, _ := Parse(testAuth("account-a", "refresh-3", "2026-08-20T00:00:00Z", "id-3"))
	_, err = CompareGeneration(saved, ambiguous)
	if !errors.Is(err, ErrAmbiguousGeneration) {
		t.Fatalf("expected ambiguity, got %v", err)
	}
}

func testAuth(account, refresh, lastRefresh, idToken string) []byte {
	return []byte(fmt.Sprintf(`{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"tokens":{"id_token":%q,"access_token":%q,"refresh_token":%q,"account_id":%q},"last_refresh":%q}`, idToken, "access-"+refresh, refresh, account, lastRefresh))
}
