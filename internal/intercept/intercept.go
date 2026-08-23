// Package intercept is the orchestration layer that ties the interception
// pieces together into the lifecycle `wiretap intercept start/stop` drives:
//
//  1. Ensure a wiretap CA exists and is trusted by this OS (castore.Installer).
//  2. Drop per-tool shim scripts into a fresh override-bin directory prepended
//     to PATH (overridebin), so git/curl/node route through the proxy and
//     trust our CA.
//  3. Append a guarded `# --wiretap-intercept--` block to the user's shell
//     startup files, activated only when WIRETAP_ACTIVE is set so normal
//     shells are unaffected.
//  4. Start the interception proxy (proxy) on a loopback port, recording
//     captures into the local PCStore via a small adapter.
//  5. Start the 127.0.0.1 control HTTP API (localapi) so external scripts and
//     the future GUI can query webhooks + captures.
//
// Start returns a Session the caller keeps until interception is over; Stop
// reverses every step (stop proxy + API, reset startup files, remove the
// override-bin dir). The CA is left installed so re-trust is cheap.
//
// The few pieces that touch the OS filesystem or spawn a terminal are thin
// glue around pure helpers (GuardBlock / Inject/RemoveStartupBlock /
// startupFilesFor / ShellCommand) which are unit-tested directly.
package intercept

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/plutack/wiretap/internal/intercept/castore"
	"github.com/plutack/wiretap/internal/intercept/localapi"
	"github.com/plutack/wiretap/internal/intercept/overridebin"
	"github.com/plutack/wiretap/internal/intercept/proxy"
	"github.com/plutack/wiretap/internal/intercept/shellscript"
	"github.com/plutack/wiretap/internal/scripting"
	"github.com/plutack/wiretap/internal/store"
	"github.com/plutack/wiretap/internal/testutil"
)

// Deps is the configuration Start wires from. Every collaborator is injected
// (Installer, PCStore, Clock) so tests run without touching the real trust
// store or the wall clock.
type Deps struct {
	ConfigDir    string                // for CA persistence + override-bin
	ProxyAddr    string                // interception proxy listen addr (use :0 for ephemeral)
	LocalAPIAddr string                // local control API listen addr
	ShellKind    shellscript.ShellKind // shell to spawn + startup files for
	Installer    castore.Installer     // CA ensure/uninstall
	PCStore      *store.PCStore        // capture sink + local API backing
	Clock        testutil.Clock        // optional; defaults to SystemClock
	Version      string                // surfaced via /local/health
	// ScriptEngine, when set, runs on_request/on_response scripts (loaded from
	// PCStore) against live intercepted traffic. Nil disables scripting — the
	// proxy then passes traffic through unmodified.
	ScriptEngine *scripting.Engine
	// OnScriptError, when set, receives every script load/run error so the
	// caller (GUI log pane, CLI stderr) can surface it. Optional.
	OnScriptError func(trigger scripting.Trigger, name string, err error)
	// StartupFiles overrides the auto-detected list (mainly for tests). Empty
	// means "resolve from the user's home for ShellKind".
	StartupFiles []string
}

// Session is a live interception: proxy + local API up, override-bin on disk,
// startup files patched. Stop reverses it.
type Session struct {
	proxy        *proxy.Proxy
	localAPI     *http.Server
	localAPILn   net.Listener
	localAPIAddr string
	overrideDir  string
	startupFiles []string

	// Session persistence: every `intercept start` run is recorded in the
	// intercept_sessions table so captures stay grouped and referenceable
	// after the session ends. sessionID is 0 when the insert failed (capture
	// still works, rows are just unsessioned).
	sessionID int64
	st        *store.PCStore
	clock     testutil.Clock
}

// Start runs the orchestration described at the package doc and returns a
// Session. On error it cleans up any half-started pieces.
func Start(ctx context.Context, deps Deps) (*Session, error) {
	if deps.Installer == nil {
		return nil, errors.New("intercept: Installer is required")
	}
	if deps.PCStore == nil {
		return nil, errors.New("intercept: PCStore is required")
	}
	clock := deps.Clock
	if clock == nil {
		clock = testutil.SystemClock{}
	}

	ca, err := deps.Installer.EnsureCA(ctx)
	if err != nil {
		return nil, fmt.Errorf("intercept: ensure CA: %w", err)
	}
	caCertPath := filepath.Join(deps.ConfigDir, "ca", "wiretap-ca.crt")

	overrideDir := filepath.Join(deps.ConfigDir, "override-bin")
	if err := overridebin.Write(overrideDir, overridebin.Env{
		ProxyAddr:       deps.ProxyAddr,
		CACertPath:      caCertPath,
		OverrideBinPath: overrideDir,
	}); err != nil {
		return nil, fmt.Errorf("intercept: write override bin: %w", err)
	}

	startupFiles := deps.StartupFiles
	if len(startupFiles) == 0 {
		startupFiles = StartupFilesFor(deps.ShellKind)
	}
	if err := injectIntoFiles(startupFiles, deps.ShellKind, shellscript.Env{
		ProxyAddr:       deps.ProxyAddr,
		OverrideBinPath: overrideDir,
		CACertPath:      caCertPath,
	}); err != nil {
		return nil, fmt.Errorf("intercept: patch startup files: %w", err)
	}

	// Record this run as an intercept session so captures are grouped in the
	// local DB. Best-effort: a failed insert degrades to unsessioned capture
	// rather than blocking interception.
	sessionID, err := deps.PCStore.CreateInterceptSession(ctx, clock.Now(), string(deps.ShellKind), deps.ProxyAddr)
	if err != nil {
		sessionID = 0
	}
	sessionStarted := false
	defer func() {
		if !sessionStarted && sessionID != 0 {
			_ = deps.PCStore.EndInterceptSession(context.Background(), sessionID, clock.Now())
		}
	}()

	recorder := &storeRecorder{st: deps.PCStore, clock: clock, sessionID: sessionID}
	proxyOpts := []proxy.Option{proxy.WithClock(clock)}
	if transformer := newScriptTransformer(deps.ScriptEngine, deps.PCStore, deps.OnScriptError); transformer != nil {
		proxyOpts = append(proxyOpts, proxy.WithTransformer(transformer))
	}
	prox := proxy.New(deps.ProxyAddr, proxy.NewCastoreSigner(ca), recorder, proxyOpts...)
	if _, err := prox.StartAsync(); err != nil {
		return nil, fmt.Errorf("intercept: start proxy: %w", err)
	}

	apiLn, err := net.Listen("tcp", deps.LocalAPIAddr)
	if err != nil {
		_ = prox.Stop(ctx)
		return nil, fmt.Errorf("intercept: listen local API: %w", err)
	}
	api := localapi.New(deps.PCStore, localapi.WithVersion(deps.Version))
	httpSrv := &http.Server{Handler: api.Routes()}
	go func() { _ = httpSrv.Serve(apiLn) }()

	sessionStarted = true
	return &Session{
		proxy:        prox,
		localAPI:     httpSrv,
		localAPILn:   apiLn,
		localAPIAddr: apiLn.Addr().String(),
		overrideDir:  overrideDir,
		startupFiles: startupFiles,
		sessionID:    sessionID,
		st:           deps.PCStore,
		clock:        clock,
	}, nil
}

// SessionID returns the intercept_sessions row id for this run, or 0 when
// session persistence failed at Start.
func (s *Session) SessionID() int64 {
	if s == nil {
		return 0
	}
	return s.sessionID
}

// ProxyAddr returns the resolved address the interception proxy is listening on
// (useful when it was started with :0). Returns "" before Start completes.
func (s *Session) ProxyAddr() string {
	if s == nil || s.proxy == nil {
		return ""
	}
	return s.proxy.Addr()
}

// LocalAPIAddr returns the resolved address the control API is listening on.
func (s *Session) LocalAPIAddr() string {
	if s == nil {
		return ""
	}
	return s.localAPIAddr
}

// Stop tears the session down: stops proxy + local API, reverts the patched
// startup files (so normal shells are clean again), and removes the per-session
// override-bin directory. The CA is left installed. Multiple calls are safe.
func (s *Session) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var errs []error
	if s.proxy != nil {
		errs = append(errs, s.proxy.Stop(ctx))
	}
	if s.localAPI != nil {
		errs = append(errs, s.localAPI.Shutdown(ctx))
	}
	if s.localAPILn != nil {
		if err := s.localAPILn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	for _, f := range s.startupFiles {
		if err := resetStartupFile(f); err != nil {
			errs = append(errs, err) // best-effort: keep going
		}
	}
	if s.overrideDir != "" {
		_ = os.RemoveAll(s.overrideDir)
	}
	// Close the session row so the history shows when this run ended. Uses a
	// background context: ctx may already be cancelled during shutdown.
	if s.st != nil && s.sessionID != 0 {
		now := time.Now()
		if s.clock != nil {
			now = s.clock.Now()
		}
		if err := s.st.EndInterceptSession(context.Background(), s.sessionID, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Cleanup reverses the persistent side effects of Start — startup-file blocks
// and the per-session override-bin directory — without a running Session. Its
// job is to let `wiretap intercept stop` recover a clean state after a crash or
// a killed session left blocks behind. It returns the startup files it tried to
// reset (whether or not they changed). The CA is left installed.
func Cleanup(kind shellscript.ShellKind, configDir string) ([]string, error) {
	files := StartupFilesFor(kind)
	for _, f := range files {
		if err := resetStartupFile(f); err != nil {
			return files, err
		}
	}
	if configDir != "" {
		_ = os.RemoveAll(filepath.Join(configDir, "override-bin"))
	}
	return files, nil
}

// SpawnShell execs an interactive shell of the given kind, inheriting the
// terminal and the WIRETAP_ACTIVE marker so its sourced startup file activates
// the gated interception block. Blocks until the shell exits. This is thin
// executive glue around ShellCommand; the construction is unit-tested there.
func (s *Session) SpawnShell(ctx context.Context, kind shellscript.ShellKind) error {
	name, args := ShellCommand(kind)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "WIRETAP_ACTIVE=1")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// --- startup-file helpers (pure or near-pure) ---------------------------

// GuardBlock returns the full delimited startup block: the SectionStart/End
// markers wrapping a per-shell guard that activates inner only when
// WIRETAP_ACTIVE is set. inner comes from shellscript.Generate.
func GuardBlock(kind shellscript.ShellKind, env shellscript.Env) (string, error) {
	inner, err := shellscript.Generate(kind, env)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(shellscript.SectionStart)
	b.WriteString("\n")
	b.WriteString(guardFor(kind, inner))
	b.WriteString(shellscript.SectionEnd)
	b.WriteString("\n")
	return b.String(), nil
}

// guardFor wraps inner in the shell-specific "if WIRETAP_ACTIVE" construct.
func guardFor(kind shellscript.ShellKind, inner string) string {
	switch kind {
	case shellscript.ShellBash, shellscript.ShellGitBash:
		return "if [ -n \"$WIRETAP_ACTIVE\" ]; then\n" + inner + "fi\n"
	case shellscript.ShellFish:
		return "if test -n \"$WIRETAP_ACTIVE\"\n" + inner + "end\n"
	case shellscript.ShellPowerShell:
		return "if ($Env:WIRETAP_ACTIVE) {\n" + inner + "}\n"
	default:
		// Unknown shell: emit the inner unguarded. Callers validate the kind
		// before reaching here in practice; this is a safe fallback.
		return inner
	}
}

// RemoveStartupBlock returns content with any wiretap gated block (markers
// inclusive) removed. If only the start marker is present without an end
// marker (a truncated or hand-edited file), the content is returned untouched
// rather than risk deleting content past the start marker.
func RemoveStartupBlock(content string) string {
	start := strings.Index(content, shellscript.SectionStart)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], shellscript.SectionEnd)
	if end < 0 {
		return content
	}
	endIdx := start + end + len(shellscript.SectionEnd)
	after := content[endIdx:]
	after = strings.TrimPrefix(after, "\n")
	return content[:start] + after
}

// InjectStartupBlock returns content with any prior wiretap block removed and
// the fresh block appended, separated by a blank line. It is idempotent.
func InjectStartupBlock(content, block string) string {
	cleaned := RemoveStartupBlock(content)
	cleaned = strings.TrimRight(cleaned, "\n")
	var b strings.Builder
	if cleaned != "" {
		b.WriteString(cleaned)
		b.WriteString("\n\n")
	}
	b.WriteString(block)
	if !strings.HasSuffix(block, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// injectIntoFiles reads each path (creating it if missing), folds in the
// guarded block, and writes it back with mode 0644. Missing parent dirs are
// created so fish's ~/.config/fish/ path works on a fresh machine.
func injectIntoFiles(files []string, kind shellscript.ShellKind, env shellscript.Env) error {
	block, err := GuardBlock(kind, env)
	if err != nil {
		return err
	}
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read %s: %w", f, err)
		}
		updated := InjectStartupBlock(string(content), block)
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			return fmt.Errorf("mkdir for %s: %w", f, err)
		}
		if err := os.WriteFile(f, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f, err)
		}
	}
	return nil
}

// resetStartupFile removes the wiretap block from f and writes the result back
// only when it changed. A missing file is a no-op.
func resetStartupFile(f string) error {
	content, err := os.ReadFile(f)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", f, err)
	}
	cleaned := RemoveStartupBlock(string(content))
	if cleaned == string(content) {
		return nil // no change
	}
	if err := os.WriteFile(f, []byte(cleaned), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", f, err)
	}
	return nil
}

// StartupFilesFor returns the candidate startup file paths for the shell the
// user runs `wiretap intercept start` under. The list is over-broad on purpose:
// patching all of them is harmless (the block gates on WIRETAP_ACTIVE) and
// makes interception robust regardless of which file the user's shell sources.
func StartupFilesFor(kind shellscript.ShellKind) []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return startupFilesFor(kind, home)
}

// startupFilesFor is the pure, home-injectable core of StartupFilesFor so tests
// don't depend on the real $HOME.
func startupFilesFor(kind shellscript.ShellKind, home string) []string {
	switch kind {
	case shellscript.ShellBash, shellscript.ShellGitBash:
		return []string{
			filepath.Join(home, ".bashrc"),
			filepath.Join(home, ".bash_profile"),
			filepath.Join(home, ".profile"),
		}
	case shellscript.ShellFish:
		return []string{filepath.Join(home, ".config", "fish", "config.fish")}
	case shellscript.ShellPowerShell:
		return []string{filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")}
	default:
		return nil
	}
}

// ShellCommand returns the executable name and args for an interactive shell of
// the given kind. SpawnShell execs this with WIRETAP_ACTIVE=1 in the env.
func ShellCommand(kind shellscript.ShellKind) (string, []string) {
	switch kind {
	case shellscript.ShellBash, shellscript.ShellGitBash:
		return "bash", []string{"-i"}
	case shellscript.ShellFish:
		return "fish", nil
	case shellscript.ShellPowerShell:
		// pwsh on Linux; fall back to the Windows-only "powershell" name in a
		// later Phase 6 pass — for the Linux-first MVP pwsh is the right call.
		return "pwsh", []string{"-NoLogo"}
	default:
		return "bash", []string{"-i"}
	}
}

// --- capture recorder --------------------------------------------------

// storeRecorder adapts *store.PCStore to proxy.Recorder, converting a
// proxy.Capture into a TrafficCaptureRow and persisting it. Headers marshal to
// JSON the same way the relay stores them (json.Marshal on http.Header).
type storeRecorder struct {
	st        *store.PCStore
	clock     testutil.Clock
	sessionID int64 // intercept_sessions row id; 0 = unsessioned
}

// Record implements proxy.Recorder. Errors from the store are returned to the
// proxy, which currently ignores them (best-effort capture); surfacing them
// here keeps the seam honest for future retry/telemetry.
func (r *storeRecorder) Record(ctx context.Context, c proxy.Capture) error {
	reqJSON, _ := json.Marshal(c.ReqHeaders)
	respJSON, _ := json.Marshal(c.RespHeaders)
	at := c.At
	if r.clock != nil {
		// Prefer the proxy's stamped time, but fall back to the injected
		// clock when the caller left it zero.
		if at.IsZero() {
			at = r.clock.Now()
		}
	}
	_, err := r.st.InsertTrafficCapture(ctx, store.TrafficCaptureRow{
		SessionID:       r.sessionID,
		At:              at,
		Method:          c.Method,
		URL:             c.URL,
		ReqHeadersJSON:  string(reqJSON),
		ReqBody:         c.ReqBody,
		Status:          c.Status,
		RespHeadersJSON: string(respJSON),
		RespBody:        c.RespBody,
	})
	return err
}
