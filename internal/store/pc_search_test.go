package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// seedCaptures inserts n captures with predictable URLs, newest last, so id
// i+1 corresponds to index i. All are 200s on distinct hosts.
func seedCaptures(t *testing.T, s *PCStore, n int) {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
			At:     fixedTime.Add(time.Duration(i) * time.Second),
			Method: "GET",
			URL:    fmt.Sprintf("https://host-%d.test/api/v1/items/%d", i, i),
			Status: 200,
		}); err != nil {
			t.Fatalf("seed capture %d: %v", i, err)
		}
	}
}

func captureIDs(page CaptureSummaryPage) []int64 {
	out := make([]int64, 0, len(page.Rows))
	for _, r := range page.Rows {
		out = append(out, r.ID)
	}
	return out
}

func wantIDs(t *testing.T, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v", got, want)
		}
	}
}

// TestSearchCaptureSummaries_ReachesRowsBeyondTheNewestPage is the regression
// this search exists to fix: a caller only ever holds the newest page of rows,
// so filtering in the caller could never see older traffic no matter how
// exactly it matched.
func TestSearchCaptureSummaries_ReachesRowsBeyondTheNewestPage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	seedCaptures(t, s, 250)

	// Rewrite the *oldest* row, far outside any default page of 100.
	if _, err := s.DB().ExecContext(ctx,
		"UPDATE traffic_captures SET url = ? WHERE id = 1",
		"https://needle.test/only-here",
	); err != nil {
		t.Fatalf("seed needle: %v", err)
	}

	recent, err := s.SearchCaptureSummaries(ctx, CaptureFilter{})
	if err != nil {
		t.Fatalf("unfiltered page: %v", err)
	}
	if len(recent.Rows) != 100 || !recent.HasMore || recent.Total != 250 {
		t.Fatalf("unfiltered page = %d rows, hasMore %v, total %d; want 100/true/250",
			len(recent.Rows), recent.HasMore, recent.Total)
	}
	for _, r := range recent.Rows {
		if strings.Contains(r.URL, "needle") {
			t.Fatal("needle is inside the newest page; test does not exercise the cap")
		}
	}

	page, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Query: "needle"})
	if err != nil {
		t.Fatalf("SearchCaptureSummaries: %v", err)
	}
	if len(page.Rows) != 1 || page.Rows[0].URL != "https://needle.test/only-here" {
		t.Fatalf("rows = %+v, want just the needle row", page.Rows)
	}
	if page.Total != 1 || page.HasMore {
		t.Errorf("total = %d, hasMore = %v; want 1, false", page.Total, page.HasMore)
	}
}

func TestSearchCaptureSummaries_Matching(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	rows := []TrafficCaptureRow{
		{Method: "GET", URL: "https://api.test/orders", Status: 200},
		{Method: "POST", URL: "https://api.test/orders", Status: 404},
		{Method: "PUT", URL: "https://other.test/percent%done", Status: 500},
		{Method: "GET", URL: "https://other.test/under_score", Status: 201},
	}
	for i, r := range rows {
		r.At = fixedTime.Add(time.Duration(i) * time.Second)
		if _, err := s.InsertTrafficCapture(ctx, r); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	cases := []struct {
		name  string
		query string
		want  []int64
	}{
		{"method, case-insensitive", "post", []int64{2}},
		{"host substring", "other.test", []int64{4, 3}},
		{"exact status code", "404", []int64{2}},
		{"status substring", "20", []int64{4, 1}},
		// instr() rather than LIKE: a user's "%" and "_" are literal characters,
		// not wildcards.
		{"percent is literal", "%", []int64{3}},
		{"underscore is literal", "_", []int64{4}},
		{"no matches", "nothing-matches-this", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Query: tc.query})
			if err != nil {
				t.Fatalf("SearchCaptureSummaries(%q): %v", tc.query, err)
			}
			wantIDs(t, captureIDs(page), tc.want)
			if page.Total != int64(len(tc.want)) {
				t.Errorf("total = %d, want %d", page.Total, len(tc.want))
			}
		})
	}
}

func TestSearchCaptureSummaries_Filters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)

	withSession, err := s.CreateInterceptSession(ctx, fixedTime, "bash", ":0")
	if err != nil {
		t.Fatalf("CreateInterceptSession: %v", err)
	}
	otherSession, err := s.CreateInterceptSession(ctx, fixedTime, "fish", ":0")
	if err != nil {
		t.Fatalf("CreateInterceptSession: %v", err)
	}

	rows := []TrafficCaptureRow{
		{SessionID: withSession, Method: "GET", URL: "https://a.test/1", Status: 200},
		{SessionID: withSession, Method: "POST", URL: "https://a.test/2", Status: 404},
		{SessionID: otherSession, Method: "GET", URL: "https://a.test/3", Status: 404},
		{SessionID: 0, Method: "GET", URL: "https://a.test/4", Status: 500},
	}
	for i, r := range rows {
		r.At = fixedTime.Add(time.Duration(i) * time.Second)
		if _, err := s.InsertTrafficCapture(ctx, r); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	cases := []struct {
		name   string
		filter CaptureFilter
		want   []int64
	}{
		{"method exact", CaptureFilter{Method: "POST"}, []int64{2}},
		{"method is case-insensitive", CaptureFilter{Method: "post"}, []int64{2}},
		// A family is a numeric range, so 5xx excludes 4xx and vice versa.
		{"status family 4xx", CaptureFilter{Status: "4xx"}, []int64{3, 2}},
		{"status family 5xx", CaptureFilter{Status: "5xx"}, []int64{4}},
		{"status family boundaries", CaptureFilter{Status: "2xx"}, []int64{1}},
		{"status exact code", CaptureFilter{Status: "404"}, []int64{3, 2}},
		{"session scoping", CaptureFilter{SessionID: withSession}, []int64{2, 1}},
		{"session plus status", CaptureFilter{SessionID: withSession, Status: "4xx"}, []int64{2}},
		{"all", CaptureFilter{}, []int64{4, 3, 2, 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.SearchCaptureSummaries(ctx, tc.filter)
			if err != nil {
				t.Fatalf("SearchCaptureSummaries: %v", err)
			}
			wantIDs(t, captureIDs(page), tc.want)
			if page.Total != int64(len(tc.want)) {
				t.Errorf("total = %d, want %d", page.Total, len(tc.want))
			}
		})
	}
}

func TestSearchCaptureSummaries_PagingAndTotal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	seedCaptures(t, s, 250)
	// Give 30 rows a 5xx status so the filtered total differs from the table.
	if _, err := s.DB().ExecContext(ctx, "UPDATE traffic_captures SET status = 503 WHERE id <= 30"); err != nil {
		t.Fatalf("seed 5xx: %v", err)
	}

	first, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Limit: 10})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Rows) != 10 || !first.HasMore || first.Total != 250 {
		t.Fatalf("first page = %d rows, hasMore %v, total %d; want 10/true/250",
			len(first.Rows), first.HasMore, first.Total)
	}

	second, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Limit: 10, BeforeID: first.Rows[9].ID})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Rows) != 10 || !second.HasMore || second.Total != 250 {
		t.Fatalf("second page = %d rows, hasMore %v, total %d; want 10/true/250",
			len(second.Rows), second.HasMore, second.Total)
	}
	if second.Rows[0].ID >= first.Rows[9].ID {
		t.Errorf("pages overlap: second starts at %d, first ended at %d", second.Rows[0].ID, first.Rows[9].ID)
	}

	// The tail of the filtered set reports no more rows rather than a full page.
	last, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Limit: 10, BeforeID: 11})
	if err != nil {
		t.Fatalf("last page: %v", err)
	}
	if len(last.Rows) != 10 || last.HasMore {
		t.Errorf("last page = %d rows, hasMore %v; want 10/false", len(last.Rows), last.HasMore)
	}

	// Total is the size of the filtered set, not the page and not the table.
	filtered, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Status: "5xx", Limit: 10})
	if err != nil {
		t.Fatalf("filtered page: %v", err)
	}
	if filtered.Total != 30 || !filtered.HasMore || len(filtered.Rows) != 10 {
		t.Errorf("filtered = %d rows, hasMore %v, total %d; want 10/true/30",
			len(filtered.Rows), filtered.HasMore, filtered.Total)
	}
}

// TestSearchCaptureSummaries_BodyOptIn pins the body behaviour: bodies are not
// searched unless asked for, and when they are, only a bounded prefix is read.
func TestSearchCaptureSummaries_BodyOptIn(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
		At:      fixedTime,
		Method:  "POST",
		URL:     "https://api.test/orders",
		ReqBody: []byte(`{"order_id":"zebraword"}`),
		Status:  201,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	without, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Query: "zebraword"})
	if err != nil {
		t.Fatalf("without body: %v", err)
	}
	if len(without.Rows) != 0 {
		t.Errorf("metadata-only search matched a body: %+v", without.Rows)
	}

	with, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Query: "zebraword", Body: true})
	if err != nil {
		t.Fatalf("with body: %v", err)
	}
	if len(with.Rows) != 1 {
		t.Fatalf("body search rows = %d, want 1", len(with.Rows))
	}

	// A token past the prefix bound is deliberately out of reach: the bound is
	// what keeps one oversized payload from dominating every scan.
	var big []byte
	big = append(big, []byte(strings.Repeat("x", searchBodyPrefix+16))...)
	big = append(big, []byte("tailword")...)
	if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
		At: fixedTime, Method: "POST", URL: "https://api.test/big", ReqBody: big, Status: 200,
	}); err != nil {
		t.Fatalf("insert big: %v", err)
	}
	deep, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Query: "tailword", Body: true})
	if err != nil {
		t.Fatalf("deep body: %v", err)
	}
	if len(deep.Rows) != 0 {
		t.Errorf("found a token past the %d byte prefix bound", searchBodyPrefix)
	}
}

func TestSearchCaptures_IncludesBodiesAndHeaders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	reqBody := []byte(`{"order_id":"zebraword"}`)
	respBody := []byte(`{"ok":true}`)
	if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
		At:              fixedTime,
		Method:          "POST",
		URL:             "https://api.test/orders",
		ReqHeadersJSON:  `{"Content-Type":["application/json"]}`,
		ReqBody:         reqBody,
		Status:          201,
		RespHeadersJSON: `{"Server":["test"]}`,
		RespBody:        respBody,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// A row with no bodies must report zero lengths rather than failing.
	if _, err := s.InsertTrafficCapture(ctx, TrafficCaptureRow{
		At: fixedTime.Add(time.Second), Method: "GET", URL: "https://api.test/empty", Status: 204,
	}); err != nil {
		t.Fatalf("insert empty: %v", err)
	}

	page, err := s.SearchCaptures(ctx, CaptureFilter{Query: "orders"})
	if err != nil {
		t.Fatalf("SearchCaptures: %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(page.Rows))
	}
	got := page.Rows[0]
	if string(got.ReqBody) != string(reqBody) || string(got.RespBody) != string(respBody) {
		t.Errorf("bodies = %q / %q, want %q / %q", got.ReqBody, got.RespBody, reqBody, respBody)
	}
	if got.Status != 201 || got.Method != "POST" || got.ReqHeadersJSON == "" || got.RespHeadersJSON == "" {
		t.Errorf("row = %+v, want populated metadata", got)
	}

	// Summaries report the same sizes without selecting the payloads.
	summaries, err := s.SearchCaptureSummaries(ctx, CaptureFilter{})
	if err != nil {
		t.Fatalf("SearchCaptureSummaries: %v", err)
	}
	if len(summaries.Rows) != 2 {
		t.Fatalf("summary rows = %d, want 2", len(summaries.Rows))
	}
	var withBody, empty *TrafficCaptureSummaryRow
	for i := range summaries.Rows {
		switch summaries.Rows[i].URL {
		case "https://api.test/orders":
			withBody = &summaries.Rows[i]
		case "https://api.test/empty":
			empty = &summaries.Rows[i]
		}
	}
	if withBody == nil || empty == nil {
		t.Fatalf("summaries = %+v, want both rows", summaries.Rows)
	}
	if withBody.ReqBodyLen != len(reqBody) || withBody.RespBodyLen != len(respBody) {
		t.Errorf("lengths = %d/%d, want %d/%d",
			withBody.ReqBodyLen, withBody.RespBodyLen, len(reqBody), len(respBody))
	}
	if empty.ReqBodyLen != 0 || empty.RespBodyLen != 0 {
		t.Errorf("NULL bodies reported %d/%d, want 0/0", empty.ReqBodyLen, empty.RespBodyLen)
	}
}

func TestSearchCaptureSummaries_RejectsInvalidStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	if _, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Status: "teapot"}); err == nil {
		t.Error("SearchCaptureSummaries accepted an invalid status filter")
	}
	// The valid forms are accepted.
	for _, ok := range []string{"2xx", "5xx", "404", " 4xx "} {
		if _, err := s.SearchCaptureSummaries(ctx, CaptureFilter{Status: ok}); err != nil {
			t.Errorf("SearchCaptureSummaries(%q): %v", ok, err)
		}
	}
}

func TestSearchWebhooks_MatchingFiltersAndPaging(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := freshPCStore(t)
	for i := range 250 {
		project := "project-a"
		path := fmt.Sprintf("/orders/%d", i)
		ip := "10.0.0.1"
		if i%2 == 0 {
			project = "project-b"
			path = fmt.Sprintf("/billing/%d", i)
			ip = "10.0.0.2"
		}
		if _, err := s.StoreWebhook(ctx, WebhookRow{
			Project: project, Seq: int64(i + 1), ReceivedAt: fixedTime,
			SourceIP: ip, Method: "POST", Path: path,
			HeadersJSON: `{}`, Body: []byte(fmt.Sprintf(`{"i":%d}`, i)),
		}, fixedTime); err != nil {
			t.Fatalf("StoreWebhook %d: %v", i, err)
		}
	}

	// Path matching reaches beyond the newest page. The expected count is
	// computed from the seed rather than hard-coded, since "/orders/7" is also a
	// prefix of "/orders/70".."/orders/79".
	wantPathMatches := 0
	for i := range 250 {
		if i%2 == 1 && strings.Contains(fmt.Sprintf("/orders/%d", i), "/orders/7") {
			wantPathMatches++
		}
	}
	path, err := s.SearchWebhooks(ctx, WebhookFilter{Query: "/orders/7"})
	if err != nil {
		t.Fatalf("path search: %v", err)
	}
	if len(path.Rows) != wantPathMatches {
		t.Fatalf("path matches = %d, want %d", len(path.Rows), wantPathMatches)
	}

	// The GUI's placeholder promises "source", so source IP must be searchable.
	ip, err := s.SearchWebhooks(ctx, WebhookFilter{Query: "10.0.0.2"})
	if err != nil {
		t.Fatalf("source ip search: %v", err)
	}
	if ip.Total != 125 || len(ip.Rows) != 100 || !ip.HasMore {
		t.Fatalf("source ip = %d rows, hasMore %v, total %d; want 100/true/125",
			len(ip.Rows), ip.HasMore, ip.Total)
	}

	project, err := s.SearchWebhooks(ctx, WebhookFilter{Project: "project-b", Query: "billing", Method: "post"})
	if err != nil {
		t.Fatalf("project search: %v", err)
	}
	if project.Total != 125 {
		t.Errorf("project total = %d, want 125", project.Total)
	}
	for _, r := range project.Rows {
		if r.Project != "project-b" {
			t.Errorf("row %s/%d leaked past the project filter", r.Project, r.Seq)
		}
	}

	other, err := s.SearchWebhooks(ctx, WebhookFilter{Project: "project-a", Method: "GET"})
	if err != nil {
		t.Fatalf("method search: %v", err)
	}
	if len(other.Rows) != 0 || other.Total != 0 {
		t.Errorf("GET matches = %d (total %d), want none", len(other.Rows), other.Total)
	}

	// Cursor paging walks the filtered set without overlap.
	first, err := s.SearchWebhooks(ctx, WebhookFilter{Project: "project-a", Limit: 5})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	second, err := s.SearchWebhooks(ctx, WebhookFilter{Project: "project-a", Limit: 5, BeforeSeq: first.Rows[4].Seq})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(first.Rows) != 5 || len(second.Rows) != 5 {
		t.Fatalf("pages = %d/%d rows, want 5/5", len(first.Rows), len(second.Rows))
	}
	if second.Rows[0].Seq >= first.Rows[4].Seq {
		t.Errorf("pages overlap: second starts at %d, first ended at %d", second.Rows[0].Seq, first.Rows[4].Seq)
	}
}
