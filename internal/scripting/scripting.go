// Package scripting embeds a sandboxed JavaScript runtime (goja, a pure-Go
// ES5.1+/ES6 interpreter — no CGO, no external runtime to bundle) so users can
// script payload transformations. A script reads and mutates an Exchange (the
// request/response state), calls built-in helpers (crypto, base64, regex,
// json, console), and may reject the exchange entirely.
//
// The package is deliberately I/O-free at its core: Run takes an in-memory
// Exchange and returns the mutated value plus a Result. That keeps the engine
// trivially table-testable and lets the interception proxy, the replayer, and
// the relay-webhook path all build on the same contract. Each Run gets a fresh
// goja.Runtime, so scripts cannot leak state between invocations, and a
// per-run timeout (enforced via goja's Interrupt) guarantees one runaway
// script never wedges the proxy.
package scripting

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dop251/goja"
)

// Trigger identifies when a script runs in the request lifecycle.
type Trigger string

// Trigger values. These mirror the stored `scripts.trigger` column.
const (
	// OnRequest runs in the interception proxy before the request goes
	// upstream. It can modify method, URL, headers, and body.
	OnRequest Trigger = "on_request"
	// OnResponse runs after the upstream responds, before the response is
	// returned to the client. It can modify status, headers, and body.
	OnResponse Trigger = "on_response"
	// OnReplay runs when replaying a webhook, before re-POSTing. It can
	// modify the stored payload (regenerate a signature, update a timestamp,
	// swap a test token).
	OnReplay Trigger = "on_replay"
	// OnWebhook runs when a webhook arrives from the relay, before it is
	// stored. It can validate, transform, or reject.
	OnWebhook Trigger = "on_webhook"
)

// Valid reports whether t is one of the known triggers.
func (t Trigger) Valid() bool {
	switch t {
	case OnRequest, OnResponse, OnReplay, OnWebhook:
		return true
	default:
		return false
	}
}

// Script is one executable unit. It maps 1:1 to a row in the SQLite `scripts`
// table (name, trigger, body, priority, enabled). Priority orders chained
// scripts sharing a trigger: lower runs first.
type Script struct {
	Name     string
	Trigger  Trigger
	Body     string
	Priority int
	Enabled  bool
}

// Result is the outcome of a single Run.
type Result struct {
	// Rejected is set when the script called reject(reason). Callers use
	// this to drop a webhook or short-circuit a chain.
	Rejected bool
	// RejectReason is the argument passed to reject(), if any.
	RejectReason string
	// Logs collects everything the script wrote via console.log/console.error,
	// in order. The GUI surfaces these in the log pane.
	Logs []string
}

// Engine runs scripts. Construct with New; it holds no per-run state, so a
// single Engine is safe to share across goroutines (each Run builds its own
// goja.Runtime).
type Engine struct {
	timeout time.Duration
}

// Option configures an Engine.
type Option func(*Engine)

// WithTimeout bounds how long a single script may run before it is
// interrupted. Non-positive values are ignored (the default is kept).
func WithTimeout(d time.Duration) Option {
	return func(e *Engine) {
		if d > 0 {
			e.timeout = d
		}
	}
}

// New builds an Engine. The default per-script timeout is 5s.
func New(opts ...Option) *Engine {
	e := &Engine{timeout: 5 * time.Second}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Run executes script against ex, mutating ex in place. The script sees two
// globals — request and response — plus the built-in helper namespaces (see
// installBuiltins). A JavaScript exception, a syntax error, a timeout, or ctx
// cancellation returns a non-nil error; ex may be partially mutated in that
// case. A clean run returns the Result (rejection state + captured logs).
func (e *Engine) Run(ctx context.Context, script string, ex *Exchange) (Result, error) {
	if ex == nil {
		return Result{}, fmt.Errorf("scripting: nil exchange")
	}
	ex.normalize()

	vm := goja.New()
	// Map struct fields to their `json` tags so scripts see request.method,
	// request.url, response.status, etc. (Go's exported names are capitalised;
	// the tags give idiomatic lowercase JS property names.)
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	if err := vm.Set("request", &ex.Request); err != nil {
		return Result{}, fmt.Errorf("scripting: bind request: %w", err)
	}
	if err := vm.Set("response", &ex.Response); err != nil {
		return Result{}, fmt.Errorf("scripting: bind response: %w", err)
	}

	var res Result
	if err := vm.Set("reject", func(reason string) {
		res.Rejected = true
		res.RejectReason = reason
	}); err != nil {
		return Result{}, fmt.Errorf("scripting: bind reject: %w", err)
	}
	if err := installBuiltins(vm, &res); err != nil {
		return Result{}, err
	}

	// Enforce the timeout / cancellation by interrupting the VM from a
	// watcher goroutine. done stops the watcher once RunString returns.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-time.After(e.timeout):
			vm.Interrupt(fmt.Errorf("scripting: timeout after %s", e.timeout))
		case <-done:
		}
	}()

	if _, err := vm.RunString(script); err != nil {
		return res, fmt.Errorf("scripting: run: %w", err)
	}
	return res, nil
}

// ScriptResult records how a single script fared inside RunChain.
type ScriptResult struct {
	Name         string
	Err          error // non-nil if the script threw / timed out
	Rejected     bool
	RejectReason string
	Logs         []string
}

// ChainResult aggregates a RunChain invocation.
type ChainResult struct {
	Results      []ScriptResult
	Rejected     bool
	RejectReason string
}

// RunChain runs every enabled script whose trigger matches, in ascending
// priority order, threading the same Exchange through each so later scripts see
// earlier mutations. A rejection short-circuits the remaining scripts. A script
// error is recorded but does NOT stop the chain — one bad script never breaks
// the pipeline (its mutations up to the failure point still stand, matching
// goja's fail-in-place semantics; callers inspect Results to surface errors).
func (e *Engine) RunChain(ctx context.Context, trigger Trigger, scripts []Script, ex *Exchange) ChainResult {
	ordered := make([]Script, 0, len(scripts))
	for _, s := range scripts {
		if s.Enabled && s.Trigger == trigger {
			ordered = append(ordered, s)
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Priority < ordered[j].Priority
	})

	var chain ChainResult
	for _, s := range ordered {
		res, err := e.Run(ctx, s.Body, ex)
		chain.Results = append(chain.Results, ScriptResult{
			Name:         s.Name,
			Err:          err,
			Rejected:     res.Rejected,
			RejectReason: res.RejectReason,
			Logs:         res.Logs,
		})
		if res.Rejected {
			chain.Rejected = true
			chain.RejectReason = res.RejectReason
			break
		}
	}
	return chain
}
