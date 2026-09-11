package secretstore

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestSystemRoundTrip(t *testing.T) {
	if os.Getenv("WIRETAP_KEYRING_INTEGRATION") == "" {
		t.Skip("set WIRETAP_KEYRING_INTEGRATION=1 to exercise the desktop keyring")
	}
	account := "integration-test/" + time.Now().UTC().Format("20060102T150405.000000000")
	store := System{}
	if err := store.Set(account, "round-trip-secret"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(account) })
	got, err := store.Get(account)
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if got != "round-trip-secret" {
		t.Fatalf("Get = %q", got)
	}
	if err := store.Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v", err)
	}
}
