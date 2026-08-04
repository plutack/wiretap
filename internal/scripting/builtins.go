package scripting

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

// installBuiltins registers the helper namespaces on vm. Each namespace is a
// plain object of Go functions; goja converts a non-nil trailing error return
// into a thrown JavaScript exception, so scripts see helper failures as normal
// JS errors they can try/catch.
//
// Provided globals:
//
//	console.log(...args) / console.error(...args) — appended to res.Logs
//	crypto.hmac(algo, key, data) -> hex string   (algo: "sha256" | "sha1")
//	crypto.sha256(data) -> hex string
//	crypto.sha1(data)   -> hex string
//	base64.encode(s) -> string / base64.decode(s) -> string
//	regex.match(pattern, s) -> bool
//	regex.replace(pattern, s, repl) -> string
//	json.parse / json.stringify — aliases of the native JSON functions
func installBuiltins(vm *goja.Runtime, res *Result) error {
	// console — capture output rather than writing to stdout so the GUI can
	// surface it and tests can assert on it.
	console := vm.NewObject()
	logFn := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			parts[i] = a.String()
		}
		res.Logs = append(res.Logs, strings.Join(parts, " "))
		return goja.Undefined()
	}
	if err := setAll(console, map[string]any{"log": logFn, "error": logFn}); err != nil {
		return err
	}
	if err := vm.Set("console", console); err != nil {
		return fmt.Errorf("scripting: set console: %w", err)
	}

	// crypto
	crypto := vm.NewObject()
	if err := setAll(crypto, map[string]any{
		"hmac": func(algo, key, data string) (string, error) {
			ctor, err := hashByName(algo)
			if err != nil {
				return "", err
			}
			mac := hmac.New(ctor, []byte(key))
			mac.Write([]byte(data))
			return hex.EncodeToString(mac.Sum(nil)), nil
		},
		"sha256": func(data string) string {
			sum := sha256.Sum256([]byte(data))
			return hex.EncodeToString(sum[:])
		},
		"sha1": func(data string) string {
			sum := sha1.Sum([]byte(data))
			return hex.EncodeToString(sum[:])
		},
	}); err != nil {
		return err
	}
	if err := vm.Set("crypto", crypto); err != nil {
		return fmt.Errorf("scripting: set crypto: %w", err)
	}

	// base64
	b64 := vm.NewObject()
	if err := setAll(b64, map[string]any{
		"encode": func(s string) string {
			return base64.StdEncoding.EncodeToString([]byte(s))
		},
		"decode": func(s string) (string, error) {
			b, err := base64.StdEncoding.DecodeString(s)
			if err != nil {
				return "", fmt.Errorf("base64.decode: %w", err)
			}
			return string(b), nil
		},
	}); err != nil {
		return err
	}
	if err := vm.Set("base64", b64); err != nil {
		return fmt.Errorf("scripting: set base64: %w", err)
	}

	// regex — backed by Go's regexp for predictable, engine-independent
	// semantics (RE2), separate from JS's native RegExp.
	rx := vm.NewObject()
	if err := setAll(rx, map[string]any{
		"match": func(pattern, s string) (bool, error) {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return false, fmt.Errorf("regex.match: %w", err)
			}
			return re.MatchString(s), nil
		},
		"replace": func(pattern, s, repl string) (string, error) {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("regex.replace: %w", err)
			}
			return re.ReplaceAllString(s, repl), nil
		},
	}); err != nil {
		return err
	}
	if err := vm.Set("regex", rx); err != nil {
		return fmt.Errorf("scripting: set regex: %w", err)
	}

	// json — expose the native JSON.parse/stringify under a lowercase alias so
	// the documented helper surface (json.parse/json.stringify) works without
	// reimplementing JSON semantics.
	if nativeJSON, ok := vm.Get("JSON").(*goja.Object); ok {
		jsonObj := vm.NewObject()
		if err := setAll(jsonObj, map[string]any{
			"parse":     nativeJSON.Get("parse"),
			"stringify": nativeJSON.Get("stringify"),
		}); err != nil {
			return err
		}
		if err := vm.Set("json", jsonObj); err != nil {
			return fmt.Errorf("scripting: set json: %w", err)
		}
	}

	return nil
}

// setAll assigns every entry of m onto obj, returning the first error.
func setAll(obj *goja.Object, m map[string]any) error {
	for name, v := range m {
		if err := obj.Set(name, v); err != nil {
			return fmt.Errorf("scripting: set %s: %w", name, err)
		}
	}
	return nil
}

// hashByName maps an algorithm name to its hash constructor.
func hashByName(algo string) (func() hash.Hash, error) {
	switch strings.ToLower(algo) {
	case "sha256":
		return sha256.New, nil
	case "sha1":
		return sha1.New, nil
	default:
		return nil, fmt.Errorf("crypto.hmac: unsupported algorithm %q", algo)
	}
}
