package overridebin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutack/wiretap/internal/testutil"
)

func fixedEnv() Env {
	return Env{
		ProxyAddr:       "127.0.0.1:8888",
		CACertPath:      "/home/u/.config/wiretap/ca/wiretap-ca.crt",
		OverrideBinPath: "/tmp/wiretap-override-001",
	}
}

func TestShim_Golden(t *testing.T) {
	t.Parallel()
	env := fixedEnv()
	for _, tc := range []struct {
		name string
		tool Tool
	}{
		{"git", ToolGit},
		{"curl", ToolCurl},
		{"node", ToolNode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Shim(tc.tool, env)
			if err != nil {
				t.Fatalf("Shim(%s): %v", tc.tool, err)
			}
			testutil.Golden(t, tc.name+".golden", got)
		})
	}
}

func TestShimContents(t *testing.T) {
	t.Parallel()
	env := fixedEnv()

	t.Run("git pins proxy and ca", func(t *testing.T) {
		t.Parallel()
		got, _ := Shim(ToolGit, env)
		for _, want := range []string{
			`http.proxy=http://127.0.0.1:8888`,
			`https.proxy=http://127.0.0.1:8888`,
			`http.sslCAInfo=/home/u/.config/wiretap/ca/wiretap-ca.crt`,
			`#!/bin/sh`,
			`__WT_OVERRIDE_DIR='/tmp/wiretap-override-001'`,
			`$d/git`, // resolver looks for the real git binary
		} {
			if !strings.Contains(got, want) {
				t.Errorf("git shim missing %q\n--- got ---\n%s", want, got)
			}
		}
	})

	t.Run("curl pins proxy and cacert with localhost bypass", func(t *testing.T) {
		t.Parallel()
		got, _ := Shim(ToolCurl, env)
		for _, want := range []string{
			`--proxy 'http://127.0.0.1:8888'`,
			`--noproxy 'localhost,127.0.0.1'`,
			`--cacert '/home/u/.config/wiretap/ca/wiretap-ca.crt'`, // includes closing quote
		} {
			if !strings.Contains(got, want) {
				t.Errorf("curl shim missing %q\n--- got ---\n%s", want, got)
			}
		}
	})

	t.Run("node is passthrough", func(t *testing.T) {
		t.Parallel()
		got, _ := Shim(ToolNode, env)
		if !strings.HasPrefix(strings.TrimSpace(got), "#!/bin/sh") {
			t.Errorf("node shim not sh: %s", got)
		}
		if !strings.Contains(got, `exec "$__WT_REAL" "$@"`) {
			t.Errorf("node shim should be a passthrough exec\n--- got ---\n%s", got)
		}
		// No proxy/ca flags on the passthrough.
		for _, bad := range []string{"--proxy", "sslCAInfo", "NODE_EXTRA"} {
			if strings.Contains(got, bad) {
				t.Errorf("node shim should not mention %q\n--- got ---\n%s", bad, got)
			}
		}
	})
}

func TestShim_SpecialCharactersAreQuoted(t *testing.T) {
	t.Parallel()
	env := Env{
		ProxyAddr:       "host with space:8888",
		CACertPath:      "/a/b'c.pem",
		OverrideBinPath: "/d/e f",
	}
	got, err := Shim(ToolCurl, env)
	if err != nil {
		t.Fatalf("Shim: %v", err)
	}
	if !strings.Contains(got, `'/d/e f'`) {
		t.Errorf("override dir not single-quoted:\n%s", got)
	}
	// The escaped single quote sequence must appear for the apostrophe path.
	if !strings.Contains(got, `'\''c.pem`) {
		t.Errorf("apostrophe in CA path not escaped:\n%s", got)
	}
}

func TestShim_UnsupportedTool(t *testing.T) {
	t.Parallel()
	if _, err := Shim(Tool("wget"), fixedEnv()); !errors.Is(err, ErrUnsupportedTool) {
		t.Errorf("Shim(wget): err = %v, want ErrUnsupportedTool", err)
	}
}

func TestWrite_CreatesExecutableShims(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	env := fixedEnv()
	// Make the override dir distinct from dir so resolve looks right.
	env.OverrideBinPath = filepath.Join(dir, "bin")

	if err := Write(filepath.Join(dir, "bin"), env); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for _, tool := range Tools() {
		p := filepath.Join(dir, "bin", string(tool))
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s perm = %o, want 0755", p, info.Mode().Perm())
		}
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if !strings.HasPrefix(string(body), "#!/bin/sh\n") {
			t.Errorf("%s missing sh shebang", p)
		}
	}
}

func TestToolsStableOrder(t *testing.T) {
	t.Parallel()
	got := Tools()
	want := []Tool{ToolGit, ToolCurl, ToolNode}
	if len(got) != len(want) {
		t.Fatalf("Tools() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Tools()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
