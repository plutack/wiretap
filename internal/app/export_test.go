package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/config"
	"github.com/plutack/wiretap/internal/store"
)

func TestApp_ExportCapture(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	id, err := a.InsertTrafficCapture(context.Background(), store.TrafficCaptureRow{
		At:              time.Now(),
		Method:          "POST",
		URL:             "https://api.example.com/v1/orders?verbose=1",
		ReqHeadersJSON:  `{"Content-Type":["application/json"],"Authorization":["Bearer tok"]}`,
		ReqBody:         []byte(`{"a":1}`),
		Status:          201,
		RespHeadersJSON: `{"Content-Type":["application/json"]}`,
		RespBody:        []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}

	out, err := a.ExportCapture(context.Background(), id, "shell", "curl")
	if err != nil {
		t.Fatalf("ExportCapture: %v", err)
	}
	for _, want := range []string{"curl", "POST", "https://api.example.com/v1/orders?verbose=1", "Authorization: Bearer tok", `{"a":1}`} {
		if !strings.Contains(out, want) {
			t.Errorf("snippet missing %q:\n%s", want, out)
		}
	}
	// The response must never leak into the exported request.
	if strings.Contains(out, `{"ok":true}`) {
		t.Errorf("snippet contains response body:\n%s", out)
	}
}

func TestApp_ExportCapture_NotFound(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	if _, err := a.ExportCapture(context.Background(), 999, "shell", "curl"); err == nil {
		t.Fatal("expected error for missing capture")
	}
}

func TestApp_ExportWebhook(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	cfg := config.Default()
	cfg.Relay.URL = "wss://relay.example.com/tunnel"
	a.cfg = &cfg

	_, err := a.store.StoreWebhook(context.Background(), store.WebhookRow{
		Project:     "project-a",
		Seq:         7,
		ReceivedAt:  time.Now(),
		Method:      "POST",
		Path:        "/orders/created",
		HeadersJSON: `{"Content-Type":["application/json"]}`,
		Body:        []byte(`{"order_id":"x"}`),
	}, time.Now())
	if err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}

	out, err := a.ExportWebhook(context.Background(), "project-a", 7, "shell", "curl")
	if err != nil {
		t.Fatalf("ExportWebhook: %v", err)
	}
	if !strings.Contains(out, "https://relay.example.com/project-a/orders/created") {
		t.Errorf("snippet should target the public ingress URL:\n%s", out)
	}
	if !strings.Contains(out, `{"order_id":"x"}`) {
		t.Errorf("snippet missing body:\n%s", out)
	}
}

func TestApp_ExportWebhook_NoRelayConfigured(t *testing.T) {
	t.Parallel()
	a, _ := openTestApp(t)
	_, err := a.store.StoreWebhook(context.Background(), store.WebhookRow{
		Project:    "p",
		Seq:        1,
		ReceivedAt: time.Now(),
		Method:     "POST",
		Body:       []byte(`x`),
	}, time.Now())
	if err != nil {
		t.Fatalf("StoreWebhook: %v", err)
	}
	out, err := a.ExportWebhook(context.Background(), "p", 1, "shell", "curl")
	if err != nil {
		t.Fatalf("ExportWebhook: %v", err)
	}
	if !strings.Contains(out, "https://relay.invalid/p") {
		t.Errorf("expected placeholder host in snippet:\n%s", out)
	}
}

func TestIngressBaseURL(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"wss://relay.example.com/tunnel", "https://relay.example.com"},
		{"ws://localhost:9000/tunnel", "http://localhost:9000"},
		{"wss://relay.example.com/base/tunnel", "https://relay.example.com/base"},
		{"https://relay.example.com", "https://relay.example.com"},
		{"", ""},
		{"::bogus::", ""},
	}
	for _, tc := range cases {
		if got := IngressBaseURL(tc.in); got != tc.want {
			t.Errorf("IngressBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
