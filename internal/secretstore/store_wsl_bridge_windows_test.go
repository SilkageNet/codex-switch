//go:build windows

package secretstore

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

func TestWindowsPowerShellDPAPIBridgeRoundTrip(t *testing.T) {
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skip("Windows PowerShell is unavailable")
	}
	store := powershellStore{binary: powershell}
	key := fmt.Sprintf("integration-test/%d", time.Now().UnixNano())
	defer func() { _ = store.Delete(key) }()

	if err := store.Set(key, "temporary-test-secret"); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if value != "temporary-test-secret" {
		t.Fatalf("unexpected DPAPI round trip value %q", value)
	}
	if err := store.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected deleted DPAPI value to be missing, got %v", err)
	}
}
