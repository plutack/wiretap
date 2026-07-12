package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update is the package flag registered via the standard flag package so
// `go test -update` regenerates golden files. We register it in an init()
// on purpose: flag.Parse happens once per test binary, so duplicate
// registration across packages is avoided by flag's own semantics.
//
//nolint:gochecknoglobals // global flag is the idiomatic pattern for -update
var update = flag.Bool("update", false, "regenerate golden files")

// Golden compares got against the snapshot at testdata/<relPath>. When run
// with -update, it (re)writes the snapshot instead of asserting, which is
// how we refresh expected output after an intentional change.
//
// relPath is relative to a testdata/ directory next to the test (the
// conventional Go layout). Callers must therefore keep their tests in the
// same directory as the code under test.
//
// Golden is safe to call from parallel subtests as long as each uses a
// distinct relPath.
func Golden(t *testing.T, relPath, got string) {
	t.Helper()
	path := filepath.Join("testdata", relPath)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden: mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("golden: write %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden: missing %q (run `go test -update` to create): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("golden mismatch for %q\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
