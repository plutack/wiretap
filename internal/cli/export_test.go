package cli

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/app"
	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/store"
)

// seedCapture opens the app against base, inserts one capture, and closes.
func seedCapture(t *testing.T, base string) int64 {
	t.Helper()
	a := app.New(config.NewManager(config.WithBaseDir(base)))
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer a.Close()
	id, err := a.InsertTrafficCapture(context.Background(), store.TrafficCaptureRow{
		At:             time.Now(),
		Method:         "POST",
		URL:            "https://api.example.com/orders",
		ReqHeadersJSON: `{"Content-Type":["application/json"]}`,
		ReqBody:        []byte(`{"a":1}`),
		Status:         200,
	})
	if err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}
	return id
}

func TestExportCaptureCmd(t *testing.T) {
	base := withTempConfigManager(t)
	id := seedCapture(t, base)

	out, _, err := runCmd(t, "dev", "export", "capture", fmt.Sprint(id), "--as", "shell/curl")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{"curl", "https://api.example.com/orders", `{"a":1}`} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestExportCaptureCmd_DefaultClient(t *testing.T) {
	base := withTempConfigManager(t)
	id := seedCapture(t, base)

	// --as python (no client) uses the target's default.
	out, _, err := runCmd(t, "dev", "export", "capture", fmt.Sprint(id), "--as", "python")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "http.client") && !strings.Contains(out, "requests") {
		t.Errorf("unexpected python snippet:\n%s", out)
	}
}

func TestExportCaptureCmd_Errors(t *testing.T) {
	withTempConfigManager(t)
	if _, _, err := runCmd(t, "dev", "export", "capture", "notanumber"); err == nil {
		t.Error("expected error for non-numeric id")
	}
	if _, _, err := runCmd(t, "dev", "export", "capture", "424242"); err == nil {
		t.Error("expected error for missing capture")
	}
}

func TestExportWebhookCmd(t *testing.T) {
	base := withTempConfigManager(t)
	a := app.New(config.NewManager(config.WithBaseDir(base)))
	if err := a.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := a.Store().StoreWebhook(context.Background(), store.WebhookRow{
		Project:     "p1",
		Seq:         1,
		ReceivedAt:  time.Now(),
		Method:      "POST",
		Path:        "/hooks/x",
		HeadersJSON: `{"Content-Type":["application/json"]}`,
		Body:        []byte(`{}`),
	}, time.Now()); err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}
	_ = a.Close()

	out, _, err := runCmd(t, "dev", "export", "webhook", "p1", "1")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "curl") || !strings.Contains(out, "/p1/hooks/x") {
		t.Errorf("unexpected snippet:\n%s", out)
	}
}

func TestExportTargetsCmd(t *testing.T) {
	withTempConfigManager(t)
	out, _, err := runCmd(t, "dev", "export", "targets")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "shell") || !strings.Contains(out, "curl*") {
		t.Errorf("targets output missing shell/curl*:\n%s", out)
	}
}
