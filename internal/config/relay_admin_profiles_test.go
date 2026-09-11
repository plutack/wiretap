package config

import (
	"os"
	"testing"
)

func TestRelayAdminProfilesRoundTrip(t *testing.T) {
	t.Parallel()
	m := NewManager(WithBaseDir(t.TempDir()))
	profiles, err := m.LoadRelayAdminProfiles()
	if err != nil || len(profiles) != 0 {
		t.Fatalf("initial profiles = %+v, %v", profiles, err)
	}
	want := []RelayAdminProfile{{ID: "relay-1", Name: "Production", RelayURL: "https://relay.example.com", LastUsed: 42}}
	if err := m.SaveRelayAdminProfiles(want); err != nil {
		t.Fatal(err)
	}
	got, err := m.LoadRelayAdminProfiles()
	if err != nil || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("profiles = %+v, %v", got, err)
	}
	path, _ := m.RelayAdminProfilesPath()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode = %v, %v", info.Mode().Perm(), err)
	}
	b, _ := os.ReadFile(path)
	if string(b) == "" {
		t.Fatal("profile file is empty")
	}
}
