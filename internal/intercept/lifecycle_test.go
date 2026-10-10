package intercept

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/intercept/shellscript"
	"github.com/plutack/wiretap/internal/store"
)

func TestExpiredShutdownStillCleansShellFinalizesAndClosesOwnedStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wiretap.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.MigratePC(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(dir, "bashrc")
	closed := 0
	sess, err := Start(context.Background(), Deps{ConfigDir: dir, ProxyAddr: "127.0.0.1:0", LocalAPIAddr: "127.0.0.1:0", ShellKind: shellscript.ShellBash, Installer: &fakeInstaller{}, PCStore: store.NewPCStore(db), StartupFiles: []string{rc}, CloseStore: func() error { closed++; return db.Close() }})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	// No accepted work: an expired drain must still perform local cleanup.
	err = sess.Stop(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatal("owned store not closed", closed)
	}
	if err := sess.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if closed != 1 {
		t.Fatal("closed twice")
	}
	contents, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), shellscript.SectionStart) {
		t.Fatal("startup patch retained")
	}
	if _, err := os.Stat(filepath.Join(dir, "override-bin")); !os.IsNotExist(err) {
		t.Fatal("wrappers retained", err)
	}
	reopened, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err := store.NewPCStore(reopened).InterceptSessions(context.Background(), 1)
	if err != nil || len(rows) != 1 || rows[0].EndedAt.IsZero() {
		t.Fatalf("unfinished session: %#v %v", rows, err)
	}
}
