package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/store"
)

// fakeQuerier is an in-memory Querier for tests: it returns canned rows and
// records the filter it was handed so handlers can assert what was forwarded.
type fakeQuerier struct {
	webhooks   []store.WebhookRow
	captures   []store.TrafficCaptureSummaryRow
	webhookErr error
	captureErr error

	gotWebhookFilter store.WebhookFilter
	gotCaptureFilter store.CaptureFilter
}

func (f *fakeQuerier) SearchWebhooks(_ context.Context, filter store.WebhookFilter) (store.WebhookPage, error) {
	f.gotWebhookFilter = filter
	if f.webhookErr != nil {
		return store.WebhookPage{}, f.webhookErr
	}
	return store.WebhookPage{Rows: f.webhooks, Total: int64(len(f.webhooks))}, nil
}

func (f *fakeQuerier) SearchCaptureSummaries(_ context.Context, filter store.CaptureFilter) (store.CaptureSummaryPage, error) {
	f.gotCaptureFilter = filter
	if f.captureErr != nil {
		return store.CaptureSummaryPage{}, f.captureErr
	}
	return store.CaptureSummaryPage{Rows: f.captures, Total: int64(len(f.captures))}, nil
}

func mustGet(t *testing.T, h http.Handler, target string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestHealth(t *testing.T) {
	t.Parallel()
	s := New(&fakeQuerier{}, WithVersion("test"))
	code, body := mustGet(t, s.Routes(), "/local/health")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if body["version"] != "test" {
		t.Errorf("version = %v, want test", body["version"])
	}
}

func TestWebhooks(t *testing.T) {
	t.Parallel()
	fq := &fakeQuerier{
		webhooks: []store.WebhookRow{
			{Project: "project-a", Seq: 7, Method: "POST", Path: "/hook", ReceivedAt: time.Unix(100, 0).UTC(), Body: []byte("abc")},
			{Project: "project-b", Seq: 1, Method: "GET", Path: "/", ReceivedAt: time.Unix(50, 0).UTC()},
		},
	}
	s := New(fq)

	code, body := mustGet(t, s.Routes(), "/local/webhooks?project=project-a&q=hook&method=POST&limit=5")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if fq.gotWebhookFilter.Project != "project-a" {
		t.Errorf("project forwarded = %q, want project-a", fq.gotWebhookFilter.Project)
	}
	if fq.gotWebhookFilter.Query != "hook" {
		t.Errorf("q forwarded = %q, want hook", fq.gotWebhookFilter.Query)
	}
	if fq.gotWebhookFilter.Method != "POST" {
		t.Errorf("method forwarded = %q, want POST", fq.gotWebhookFilter.Method)
	}
	if fq.gotWebhookFilter.Limit != 5 {
		t.Errorf("limit forwarded = %d, want 5", fq.gotWebhookFilter.Limit)
	}
	whs, _ := body["webhooks"].([]any)
	if len(whs) != 2 {
		t.Fatalf("webhooks = %d, want 2", len(whs))
	}
	if body["total"] != float64(2) {
		t.Errorf("total = %v, want 2", body["total"])
	}
	first, _ := whs[0].(map[string]any)
	if first["project"] != "project-a" {
		t.Errorf("first project = %v", first["project"])
	}
	if first["body_len"] != float64(3) {
		t.Errorf("body_len = %v, want 3", first["body_len"])
	}
}

func TestCaptures(t *testing.T) {
	t.Parallel()
	fq := &fakeQuerier{
		captures: []store.TrafficCaptureSummaryRow{
			{ID: 42, At: time.Unix(200, 0).UTC(), Method: "POST", URL: "https://x/y", Status: 200, ReqBodyLen: 2, RespBodyLen: 2},
		},
	}
	s := New(fq)

	// The unrecognised parameter must be ignored rather than rejected.
	code, body := mustGet(t, s.Routes(), "/local/captures?q=x%2Fy&method=POST&status=2xx&limit=50&nonsense=1")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if fq.gotCaptureFilter.Query != "x/y" {
		t.Errorf("q forwarded = %q, want x/y", fq.gotCaptureFilter.Query)
	}
	if fq.gotCaptureFilter.Method != "POST" {
		t.Errorf("method forwarded = %q, want POST", fq.gotCaptureFilter.Method)
	}
	if fq.gotCaptureFilter.Status != "2xx" {
		t.Errorf("status forwarded = %q, want 2xx", fq.gotCaptureFilter.Status)
	}
	if fq.gotCaptureFilter.Limit != 50 {
		t.Errorf("limit forwarded = %d, want 50", fq.gotCaptureFilter.Limit)
	}
	caps, _ := body["captures"].([]any)
	if len(caps) != 1 {
		t.Fatalf("captures = %d, want 1", len(caps))
	}
	c0, _ := caps[0].(map[string]any)
	if c0["id"] != float64(42) {
		t.Errorf("id = %v, want 42", c0["id"])
	}
	if c0["url"] != "https://x/y" {
		t.Errorf("url = %v", c0["url"])
	}
	if c0["req_body_len"] != float64(2) {
		t.Errorf("req_body_len = %v, want 2", c0["req_body_len"])
	}
}

func TestLimitClamping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		target string
		want   int
	}{
		{"empty → default 100", "/local/captures", 100},
		{"invalid → default 100", "/local/captures?limit=abc", 100},
		{"zero → default 100", "/local/captures?limit=0", 100},
		{"negative → default 100", "/local/captures?limit=-3", 100},
		{"huge → capped 1000", "/local/captures?limit=999999", 1000},
		{"normal", "/local/captures?limit=25", 25},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fq := &fakeQuerier{captures: nil}
			s := New(fq)
			_, _ = mustGet(t, s.Routes(), tc.target)
			if fq.gotCaptureFilter.Limit != tc.want {
				t.Errorf("limit = %d, want %d", fq.gotCaptureFilter.Limit, tc.want)
			}
		})
	}
}

func TestQueryErrorsReturn500(t *testing.T) {
	t.Parallel()
	t.Run("webhooks", func(t *testing.T) {
		t.Parallel()
		fq := &fakeQuerier{webhookErr: errors.New(" boom")}
		s := New(fq)
		code, body := mustGet(t, s.Routes(), "/local/webhooks")
		if code != http.StatusInternalServerError {
			t.Fatalf("code = %d, want 500", code)
		}
		if body["error"] == nil {
			t.Error("missing error field")
		}
	})
	t.Run("captures", func(t *testing.T) {
		t.Parallel()
		fq := &fakeQuerier{captureErr: errors.New(" boom")}
		s := New(fq)
		code, _ := mustGet(t, s.Routes(), "/local/captures")
		if code != http.StatusInternalServerError {
			t.Fatalf("code = %d, want 500", code)
		}
	})
}
