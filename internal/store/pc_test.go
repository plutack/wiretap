package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func freshPCStore(t *testing.T) *PCStore {
	t.Helper()
	ctx := context.Background()
	db, err := OpenInMemory(fmt.Sprintf("pc-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := MigratePC(ctx, db); err != nil {
		t.Fatalf("MigratePC: %v", err)
	}
	return NewPCStore(db)
}

func TestPCStore_StoreWebhook_RawHeadersAndBodyPreserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	rawHeaders := []byte("X-Forwarded-For: 10.0.0.1\r\nX-Forwarded-For: 10.0.0.2\r\nX-Event: created\r\n")
	body := []byte{0x00, 0x01, 0xFF, 'b', 'i', 'n'}
	inserted, err := s.StoreWebhook(ctx, WebhookRow{
		Project: "project-a", Seq: 7, ReceivedAt: fixedTime,
		Method: "POST", Path: "/x", HeadersJSON: `{"X-Event":["created"]}`,
		RawHeaders: rawHeaders, Body: body,
	}, fixedTime)
	if err != nil || !inserted {
		t.Fatalf("StoreWebhook: inserted=%v err=%v", inserted, err)
	}
	row, err := s.WebhookBySeq(ctx, "project-a", 7)
	if err != nil {
		t.Fatalf("WebhookBySeq: %v", err)
	}
	if !bytes.Equal(row.RawHeaders, rawHeaders) {
		t.Errorf("RawHeaders mismatch\n want %q\n  got %q", rawHeaders, row.RawHeaders)
	}
	if !bytes.Equal(row.Body, body) {
		t.Errorf("Body mismatch\n want %x\n  got %x", body, row.Body)
	}
	// Also confirm the list query surfaces raw_headers.
	rows, err := s.Webhooks(ctx, "project-a", 0)
	if err != nil {
		t.Fatalf("Webhooks: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if !bytes.Equal(rows[0].RawHeaders, rawHeaders) {
		t.Errorf("list query RawHeaders mismatch")
	}
}

func TestPCStore_StoreWebhook_AndLastSeq(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)

	for _, seq := range []int64{1, 2, 3} {
		inserted, err := s.StoreWebhook(ctx, WebhookRow{
			Project: "project-a", Seq: seq, ReceivedAt: fixedTime, Method: "POST", HeadersJSON: "{}", Body: []byte("body"),
		}, fixedTime.Add(time.Duration(seq)*time.Second))
		if err != nil {
			t.Fatalf("StoreWebhook seq=%d: %v", seq, err)
		}
		if !inserted {
			t.Errorf("seq=%d: inserted = false, want true", seq)
		}
	}
	got, err := s.LastSeq(ctx, "project-a")
	if err != nil {
		t.Fatalf("LastSeq: %v", err)
	}
	if got != 3 {
		t.Errorf("LastSeq = %d, want 3", got)
	}
	// empty project
	got, err = s.LastSeq(ctx, "ghost")
	if err != nil {
		t.Fatalf("LastSeq ghost: %v", err)
	}
	if got != 0 {
		t.Errorf("LastSeq ghost = %d, want 0", got)
	}
}

func TestPCStore_StoreWebhook_IdempotentDuplicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	w := WebhookRow{Project: "p", Seq: 7, ReceivedAt: fixedTime, Method: "POST", HeadersJSON: "{}", Body: []byte("x")}
	if inserted, err := s.StoreWebhook(ctx, w, fixedTime); err != nil || !inserted {
		t.Fatalf("first insert: inserted=%v err=%v", inserted, err)
	}
	// Re-insert same (project, seq) → ignored, not error
	inserted, err := s.StoreWebhook(ctx, w, fixedTime.Add(time.Minute))
	if err != nil {
		t.Fatalf("second insert err: %v", err)
	}
	if inserted {
		t.Errorf("duplicate insert: inserted=true, want false")
	}
}

func TestPCStore_Webhooks_ListAndFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	for _, p := range []string{"alpha", "alpha", "beta"} {
		seq, _ := s.LastSeq(ctx, p)
		seq++
		_, _ = s.StoreWebhook(ctx, WebhookRow{Project: p, Seq: seq, ReceivedAt: fixedTime, Method: "POST", HeadersJSON: "{}"}, fixedTime)
	}
	got, err := s.Webhooks(ctx, "alpha", 0)
	if err != nil {
		t.Fatalf("Webhooks alpha: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("alpha count = %d, want 2", len(got))
	}
	all, err := s.Webhooks(ctx, "", 0)
	if err != nil {
		t.Fatalf("Webhooks all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all count = %d, want 3", len(all))
	}
	// limit is honoured
	lim, err := s.Webhooks(ctx, "", 2)
	if err != nil {
		t.Fatalf("Webhooks limit: %v", err)
	}
	if len(lim) != 2 {
		t.Errorf("limit count = %d, want 2", len(lim))
	}
	// order is newest-first (descending seq)
	if lim[0].Seq != 2 {
		t.Errorf("first row seq = %d, want 2", lim[0].Seq)
	}
}

func TestPCStore_WebhookBySeq(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	_, _ = s.StoreWebhook(ctx, WebhookRow{Project: "p", Seq: 42, ReceivedAt: fixedTime, Method: "POST", Path: "/x", HeadersJSON: "{}", Body: []byte("body")}, fixedTime)
	got, err := s.WebhookBySeq(ctx, "p", 42)
	if err != nil {
		t.Fatalf("WebhookBySeq: %v", err)
	}
	if got.Path != "/x" || string(got.Body) != "body" {
		t.Errorf("row = %+v", got)
	}
	if _, err := s.WebhookBySeq(ctx, "p", 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestPCStore_InsertTrafficCapture_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	id, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
		At: fixedTime, Method: "GET", URL: "https://example.com",
		ReqHeadersJSON: `{"A":["b"]}`, ReqBody: []byte("req"),
		Status: 200, RespHeadersJSON: `{"C":["d"]}`, RespBody: []byte("resp"),
	})
	if err != nil {
		t.Fatalf("InsertTrafficCapture: %v", err)
	}
	if id <= 0 {
		t.Errorf("id = %d, want > 0", id)
	}
	got, err := s.TrafficCaptures(ctx, 0)
	if err != nil {
		t.Fatalf("TrafficCaptures: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("count = %d, want 1", len(got))
	}
	if got[0].Method != "GET" || got[0].URL != "https://example.com" || got[0].Status != 200 {
		t.Errorf("row = %+v", got[0])
	}
	if string(got[0].ReqBody) != "req" || string(got[0].RespBody) != "resp" {
		t.Errorf("bodies = req=%q resp=%q", got[0].ReqBody, got[0].RespBody)
	}
}

func TestPCStore_TrafficCaptures_OrderAndLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	for i := 0; i < 5; i++ {
		_, _ = s.InsertTrafficCapture(ctx, TrafficCaptureRow{
			At:     fixedTime.Add(time.Duration(i) * time.Second),
			Method: "GET", URL: "https://example.com",
		})
	}
	got, _ := s.TrafficCaptures(ctx, 3)
	if len(got) != 3 {
		t.Fatalf("limit=3 got %d rows", len(got))
	}
	if got[0].ID != 5 || got[2].ID != 3 {
		t.Errorf("ordering = [%d, %d, %d], want [5, 4, 3]", got[0].ID, got[1].ID, got[2].ID)
	}
}
