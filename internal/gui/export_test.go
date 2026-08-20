package gui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/store"
)

func TestBindings_ExportTargets(t *testing.T) {
	t.Parallel()
	b, _ := newBindings(t)
	ts, err := b.ExportTargets()
	if err != nil {
		t.Fatalf("ExportTargets: %v", err)
	}
	if len(ts) < 10 {
		t.Fatalf("target catalog too small: %d", len(ts))
	}
	var shell *TargetView
	for i := range ts {
		if ts[i].Key == "shell" {
			shell = &ts[i]
		}
	}
	if shell == nil || shell.DefaultClient != "curl" || len(shell.Clients) == 0 {
		t.Errorf("shell target malformed: %+v", shell)
	}
}

func TestBindings_ExportCapture(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
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
	out, err := b.ExportCapture(id, "shell", "curl")
	if err != nil {
		t.Fatalf("ExportCapture: %v", err)
	}
	if !strings.Contains(out, "curl") || !strings.Contains(out, "https://api.example.com/orders") {
		t.Errorf("unexpected snippet:\n%s", out)
	}
	if _, err := b.ExportCapture(9999, "shell", "curl"); err == nil {
		t.Error("expected error for missing capture")
	}
}

func TestBindings_ExportWebhook(t *testing.T) {
	t.Parallel()
	b, a := newBindings(t)
	if _, err := a.Store().StoreWebhook(context.Background(), store.WebhookRow{
		Project:     "p1",
		Seq:         3,
		ReceivedAt:  time.Now(),
		Method:      "POST",
		Path:        "/orders/created",
		HeadersJSON: `{"Content-Type":["application/json"]}`,
		Body:        []byte(`{"x":1}`),
	}, time.Now()); err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}
	out, err := b.ExportWebhook("p1", 3, "javascript", "fetch")
	if err != nil {
		t.Fatalf("ExportWebhook: %v", err)
	}
	if !strings.Contains(out, "fetch(") || !strings.Contains(out, "/p1/orders/created") {
		t.Errorf("unexpected snippet:\n%s", out)
	}
}
