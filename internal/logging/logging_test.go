package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestFormatsLevelsAndRedaction(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		for _, level := range []string{"debug", "info", "warn", "error"} {
			t.Run(format+"/"+level, func(t *testing.T) {
				var out bytes.Buffer
				logger, err := New(&out, format, level)
				if err != nil {
					t.Fatal(err)
				}
				logger.Debug("debug_event")
				logger.Info("info_event")
				logger.Warn("warn_event")
				logger.Error("error_event", "admin_token", "super-secret", slog.Group("nested", "Authorization", "Bearer hidden"))
				text := out.String()
				if strings.Contains(text, "super-secret") || strings.Contains(text, "Bearer hidden") {
					t.Fatal("sensitive field leaked")
				}
				for event, want := range map[string]bool{"debug_event": level == "debug", "info_event": level == "debug" || level == "info", "warn_event": level != "error", "error_event": true} {
					if strings.Contains(text, event) != want {
						t.Errorf("%s visibility: %s", event, text)
					}
				}
				if format == "json" {
					for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
						var record map[string]any
						if err := json.Unmarshal([]byte(line), &record); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
		}
	}
}

func TestInvalidConfiguration(t *testing.T) {
	for _, pair := range [][2]string{{"xml", "info"}, {"text", "trace"}, {"secret-format", "secret-level"}} {
		_, err := New(&bytes.Buffer{}, pair[0], pair[1])
		if err == nil {
			t.Fatal("accepted invalid config")
		}
		if strings.Contains(err.Error(), "secret-") {
			t.Fatal("echoed invalid value")
		}
	}
	if _, err := New(nil, "text", "info"); err == nil {
		t.Fatal("accepted nil output")
	}
}

func TestDefaultsAndIsolation(t *testing.T) {
	before := slog.Default()
	var out bytes.Buffer
	logger, err := New(&out, "", "")
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("hidden")
	logger.Info("visible")
	if strings.Contains(out.String(), "hidden") || !strings.Contains(out.String(), "visible") {
		t.Fatal(out.String())
	}
	if slog.Default() != before {
		t.Fatal("changed default logger")
	}
}
