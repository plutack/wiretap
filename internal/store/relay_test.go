package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// freshRelayStore stands up a migrated in-memory RelayStore for one test.
// Using a unique name per test avoids cross-test pollution even without
// closing the handle, but t.Cleanup closes it anyway.
func freshRelayStore(t *testing.T) *RelayStore {
	t.Helper()
	ctx := context.Background()
	db, err := OpenInMemory(fmt.Sprintf("relay-%s", t.Name()))
	if err != nil {
		t.Fatalf("OpenInMemory: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := MigrateRelay(ctx, db); err != nil {
		t.Fatalf("MigrateRelay: %v", err)
	}
	return NewRelayStore(db)
}

var fixedTime = time.Unix(1_700_000_000, 0).UTC()

func TestRelayStore_CreateClient_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)

	if err := s.CreateClient(ctx, "client-1", "tok-1", "laptop", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	got, err := s.Client(ctx, "client-1")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if got.ClientID != "client-1" || got.ClientToken != "tok-1" || got.DisplayName != "laptop" {
		t.Errorf("Client = %+v, mismatch", got)
	}
	if !got.CreatedAt.Equal(fixedTime) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, fixedTime)
	}
	if !got.LastSeenAt.IsZero() {
		t.Errorf("LastSeenAt = %v, want zero", got.LastSeenAt)
	}
}

func TestRelayStore_CreateClient_DuplicateIsConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "n", fixedTime); err != nil {
		t.Fatalf("first CreateClient: %v", err)
	}
	err := s.CreateClient(ctx, "c1", "other", "n", fixedTime)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second CreateClient: err = %v, want ErrConflict", err)
	}
}

func TestRelayStore_Client_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if _, err := s.Client(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRelayStore_TouchClient(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	seen := fixedTime.Add(2 * time.Hour)
	if err := s.TouchClient(ctx, "c1", seen); err != nil {
		t.Fatalf("TouchClient: %v", err)
	}
	got, err := s.Client(ctx, "c1")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if !got.LastSeenAt.Equal(seen) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, seen)
	}
}

func TestRelayStore_BindProject_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	p, err := s.Project(ctx, "project-a")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.ClientID != "c1" || p.AckedSeq != 0 {
		t.Errorf("Project = %+v", p)
	}
}

func TestRelayStore_BindProject_DuplicateIsConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.CreateClient(ctx, "c2", "t2", "", fixedTime); err != nil {
		t.Fatalf("CreateClient c2: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("first BindProject: %v", err)
	}
	err := s.BindProject(ctx, "project-a", "c2", fixedTime)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second BindProject: err = %v, want ErrConflict", err)
	}
}

func TestRelayStore_ProjectsByClient_Sorted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	for _, p := range []string{"gamma", "alpha", "beta"} {
		if err := s.BindProject(ctx, p, "c1", fixedTime); err != nil {
			t.Fatalf("BindProject %s: %v", p, err)
		}
	}
	got, err := s.ProjectsByClient(ctx, "c1")
	if err != nil {
		t.Fatalf("ProjectsByClient: %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRelayStore_ReclaimProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient c1: %v", err)
	}
	if err := s.CreateClient(ctx, "c2", "t2", "", fixedTime); err != nil {
		t.Fatalf("CreateClient c2: %v", err)
	}
	if err := s.BindProject(ctx, "p", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	if err := s.ReclaimProject(ctx, "p", "c2", fixedTime.Add(time.Hour)); err != nil {
		t.Fatalf("ReclaimProject: %v", err)
	}
	p, err := s.Project(ctx, "p")
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if p.ClientID != "c2" {
		t.Errorf("after reclaim, owner = %q, want %q", p.ClientID, "c2")
	}
}

func TestRelayStore_ReclaimProject_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.ReclaimProject(ctx, "ghost", "c1", fixedTime); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRelayStore_DeleteClient_Cascades(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	if err := s.DeleteClient(ctx, "c1"); err != nil {
		t.Fatalf("DeleteClient: %v", err)
	}
	if _, err := s.Project(ctx, "project-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Project after delete: err = %v, want ErrNotFound", err)
	}
}

func TestRelayStore_NextWebhookSeq_Monotonic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	for i, want := range []int64{1, 2, 3, 4, 5} {
		got, err := s.NextWebhookSeq(ctx, "project-a")
		if err != nil {
			t.Fatalf("NextWebhookSeq[%d]: %v", i, err)
		}
		if got != want {
			t.Errorf("NextWebhookSeq[%d] = %d, want %d", i, got, want)
		}
	}
	// acked_seq is the PC's delivery cursor, NOT the allocation counter.
	// Five un-acked allocations must leave it at 0. Earlier versions conflated
	// these two columns; this assertion locks the corrected semantics so the
	// bug cannot silently come back.
	seq, err := s.AckedSeq(ctx, "project-a")
	if err != nil {
		t.Fatalf("AckedSeq: %v", err)
	}
	if seq != 0 {
		t.Errorf("AckedSeq = %d, want 0 (allocation must not advance the delivery cursor)", seq)
	}
}

// TestRelayStore_NextWebhookSeq_AndMarkDelivered_AreDecoupled confirms the
// fixed semantics end-to-end: allocating 3 webhooks, acking only up to seq=2,
// leaves acked_seq=2 while NextWebhookSeq continues to allocate 4, 5, ...
// This is the invariant the tunnel protocol relies on.
func TestRelayStore_NextWebhookSeq_AndMarkDelivered_AreDecoupled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	for i := 0; i < 3; i++ {
		seq, err := s.NextWebhookSeq(ctx, "project-a")
		if err != nil {
			t.Fatalf("NextWebhookSeq[%d]: %v", i, err)
		}
		if err := s.InsertWebhook(ctx, WebhookRow{
			Project: "project-a", Seq: seq, ReceivedAt: fixedTime,
			Method: "POST", HeadersJSON: "{}",
		}); err != nil {
			t.Fatalf("InsertWebhook[%d]: %v", i, err)
		}
	}
	if err := s.MarkDelivered(ctx, "project-a", 2, fixedTime.Add(time.Minute)); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if seq, _ := s.AckedSeq(ctx, "project-a"); seq != 2 {
		t.Errorf("acked_seq after ack=2 = %d, want 2", seq)
	}
	// Further allocations continue monotonically and do not touch the cursor.
	next, err := s.NextWebhookSeq(ctx, "project-a")
	if err != nil {
		t.Fatalf("NextWebhookSeq after ack: %v", err)
	}
	if next != 4 {
		t.Errorf("next allocation = %d, want 4 (3 already allocated)", next)
	}
	if seq, _ := s.AckedSeq(ctx, "project-a"); seq != 2 {
		t.Errorf("acked_seq touched by allocation = %d, want unchanged at 2", seq)
	}
}

func TestRelayStore_NextWebhookSeq_UnknownProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if _, err := s.NextWebhookSeq(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRelayStore_InsertAndRetrieve_Webhook_RawHeadersAndBodyPreserved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	seq, err := s.NextWebhookSeq(ctx, "project-a")
	if err != nil {
		t.Fatalf("NextWebhookSeq: %v", err)
	}
	// Raw header block with a duplicate header, exactly as a sender would emit.
	rawHeaders := []byte("X-Forwarded-For: 10.0.0.1\r\nX-Forwarded-For: 10.0.0.2\r\nContent-Type: application/json\r\n")
	// Binary-ish body to confirm BLOB byte-exactness.
	body := []byte{0x00, 0x01, 0xFF, 'h', 'e', 'l', 'l', 'o'}
	w := WebhookRow{
		Project: "project-a", Seq: seq,
		ReceivedAt: fixedTime, SourceIP: "10.0.0.1", Method: "POST",
		Path: "/orders/42", HeadersJSON: `{"X-Forwarded-For":["10.0.0.1","10.0.0.2"],"Content-Type":["application/json"]}`,
		RawHeaders: rawHeaders, Body: body,
	}
	if err := s.InsertWebhook(ctx, w); err != nil {
		t.Fatalf("InsertWebhook: %v", err)
	}
	got, err := s.WebhooksAfter(ctx, "project-a", 0)
	if err != nil {
		t.Fatalf("WebhooksAfter: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	row := got[0]
	if !bytes.Equal(row.RawHeaders, rawHeaders) {
		t.Errorf("RawHeaders mismatch\n want %q\n  got %q", rawHeaders, row.RawHeaders)
	}
	if !bytes.Equal(row.Body, body) {
		t.Errorf("Body mismatch\n want %x\n  got %x", body, row.Body)
	}
	if row.Method != "POST" {
		t.Errorf("Method = %q, want POST", row.Method)
	}
}

func TestRelayStore_InsertAndRetrieve_Webhook(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	seq, err := s.NextWebhookSeq(ctx, "project-a")
	if err != nil {
		t.Fatalf("NextWebhookSeq: %v", err)
	}
	w := WebhookRow{
		Project: "project-a", Seq: seq,
		ReceivedAt: fixedTime, SourceIP: "10.0.0.1", Method: "POST",
		Path: "/orders/42", HeadersJSON: `{"X":["y"]}`, Body: []byte(`{"ok":true}`),
	}
	if err := s.InsertWebhook(ctx, w); err != nil {
		t.Fatalf("InsertWebhook: %v", err)
	}
	got, err := s.WebhooksAfter(ctx, "project-a", 0)
	if err != nil {
		t.Fatalf("WebhooksAfter: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Seq != seq || got[0].Method != "POST" || string(got[0].Body) != `{"ok":true}` {
		t.Errorf("row = %+v", got[0])
	}
}

func TestRelayStore_WebhooksAfter_Cursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	for range [3]struct{}{} {
		seq, err := s.NextWebhookSeq(ctx, "project-a")
		if err != nil {
			t.Fatalf("NextWebhookSeq: %v", err)
		}
		if err := s.InsertWebhook(ctx, WebhookRow{Project: "project-a", Seq: seq, ReceivedAt: fixedTime, Method: "POST", HeadersJSON: "{}"}); err != nil {
			t.Fatalf("InsertWebhook: %v", err)
		}
	}
	got, err := s.WebhooksAfter(ctx, "project-a", 1)
	if err != nil {
		t.Fatalf("WebhooksAfter: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("after seq=1 got %d rows, want 2", len(got))
	}
	if got[0].Seq != 2 || got[1].Seq != 3 {
		t.Errorf("ordering = [%d, %d], want [2, 3]", got[0].Seq, got[1].Seq)
	}
}

func TestRelayStore_MarkDelivered_AndPending(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	for range [3]struct{}{} {
		seq, _ := s.NextWebhookSeq(ctx, "project-a")
		_ = s.InsertWebhook(ctx, WebhookRow{Project: "project-a", Seq: seq, ReceivedAt: fixedTime, Method: "POST", HeadersJSON: "{}"})
	}
	if got, _ := s.PendingCount(ctx, "project-a"); got != 3 {
		t.Fatalf("pending before ack = %d, want 3", got)
	}
	if err := s.MarkDelivered(ctx, "project-a", 2, fixedTime.Add(time.Minute)); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if got, _ := s.PendingCount(ctx, "project-a"); got != 1 {
		t.Errorf("pending after ack=2 = %d, want 1", got)
	}
	// idempotent: re-ack old seq is a no-op
	if err := s.MarkDelivered(ctx, "project-a", 1, fixedTime); err != nil {
		t.Errorf("re-ack: %v", err)
	}
}

func TestRelayStore_VacuumDelivered(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "project-a", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	for range [3]struct{}{} {
		seq, _ := s.NextWebhookSeq(ctx, "project-a")
		_ = s.InsertWebhook(ctx, WebhookRow{Project: "project-a", Seq: seq, ReceivedAt: fixedTime, Method: "POST", HeadersJSON: "{}"})
	}
	_ = s.MarkDelivered(ctx, "project-a", 2, fixedTime.Add(time.Minute))
	n, err := s.VacuumDelivered(ctx, fixedTime.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("VacuumDelivered: %v", err)
	}
	if n != 2 {
		t.Errorf("vacuumed = %d, want 2", n)
	}
	// remains one undelivered webhook
	if got, _ := s.PendingCount(ctx, "project-a"); got != 1 {
		t.Errorf("pending after vacuum = %d, want 1", got)
	}
}

func TestRelayStore_ClientByProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshRelayStore(t)
	if err := s.CreateClient(ctx, "c1", "t", "", fixedTime); err != nil {
		t.Fatalf("CreateClient: %v", err)
	}
	if err := s.BindProject(ctx, "p", "c1", fixedTime); err != nil {
		t.Fatalf("BindProject: %v", err)
	}
	got, err := s.ClientByProject(ctx, "p")
	if err != nil {
		t.Fatalf("ClientByProject: %v", err)
	}
	if got != "c1" {
		t.Errorf("got %q, want %q", got, "c1")
	}
	if _, err := s.ClientByProject(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
