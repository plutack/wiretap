package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/plutack/wiretap/internal/store"
)

// TestRenderCols_ExactWidthAndAlignment pins the layout contract every table
// row depends on: the line is exactly as wide as asked, fixed columns keep
// their width, and right-aligned cells end where they should.
func TestRenderCols_ExactWidthAndAlignment(t *testing.T) {
	t.Parallel()
	cells := []colSpec{
		{width: 4, text: "GET"},
		{text: "flexible"},
		{width: 5, right: true, text: "9 B"},
	}
	for _, width := range []int{40, 60, 100} {
		got := renderCols(cells, width)
		if w := lipgloss.Width(got); w != width {
			t.Errorf("width %d: rendered %d cells (%q)", width, w, got)
		}
		if !strings.HasPrefix(got, "GET ") {
			t.Errorf("width %d: first column moved: %q", width, got)
		}
		if !strings.HasSuffix(got, "  9 B") {
			t.Errorf("width %d: last column not right-aligned: %q", width, got)
		}
	}
}

// TestRenderCols_TruncatesWithEllipsis: an over-long cell is cut, not allowed
// to push the columns after it out of place.
func TestRenderCols_TruncatesWithEllipsis(t *testing.T) {
	t.Parallel()
	got := renderCols([]colSpec{
		{text: strings.Repeat("x", 200)},
		{width: 5, right: true, text: "1 B"},
	}, 30)
	if w := lipgloss.Width(got); w != 30 {
		t.Errorf("rendered %d cells, want 30: %q", w, got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("long cell was not ellipsised: %q", got)
	}
	if !strings.HasSuffix(got, "  1 B") {
		t.Errorf("trailing column shifted: %q", got)
	}
}

// TestCaptureRows_ColumnsStayPutRegardlessOfURLLength is the regression for the
// reported bug: the size and time columns used to follow the URL, because the
// URL cell was truncated but never padded, so their position depended on how
// long each URL happened to be.
func TestCaptureRows_ColumnsStayPutRegardlessOfURLLength(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 24, 15, 59, 35, 0, time.UTC)
	short := captureItem{row: store.TrafficCaptureSummaryRow{
		Method: "POST", Status: 200, URL: "https://a.test/x", At: at, ReqBodyLen: 198, RespBodyLen: 1900,
	}}
	long := captureItem{row: store.TrafficCaptureSummaryRow{
		Method: "POST", Status: 200,
		URL: "https://auth-sandbox.fuse.me/oauth/token/with/a/deliberately/long/path",
		At:  at, ReqBodyLen: 198, RespBodyLen: 1900,
	}}

	const width = 120
	var a, b bytes.Buffer
	renderCaptureRow(&a, short, false, width, darkTheme())
	renderCaptureRow(&b, long, false, width, darkTheme())

	// Both lines must be the same width, and the transfer/timestamp columns must
	// begin at the same cell in each. Rows carry a two-cell cursor gutter on top
	// of the table width.
	lineA, lineB := a.String(), b.String()
	if lipgloss.Width(lineA) != width+2 || lipgloss.Width(lineB) != width+2 {
		t.Fatalf("row widths = %d / %d, want %d\n%q\n%q",
			lipgloss.Width(lineA), lipgloss.Width(lineB), width+2, lineA, lineB)
	}
	idxA, idxB := strings.Index(lineA, "↑"), strings.Index(lineB, "↑")
	if idxA < 0 || idxB < 0 {
		t.Fatalf("transfer column missing:\n%q\n%q", lineA, lineB)
	}
	if idxA != idxB {
		t.Errorf("transfer column starts at %d for a short URL and %d for a long one; it must not move\n%q\n%q",
			idxA, idxB, lineA, lineB)
	}
	tailA, tailB := lineA[len(lineA)-8:], lineB[len(lineB)-8:]
	if tailA != tailB {
		t.Errorf("timestamp column moved: %q vs %q", tailA, tailB)
	}
}

// TestWebhookRows_ColumnsStayPut is the same regression for the ingress table.
func TestWebhookRows_ColumnsStayPut(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 9, 24, 13, 42, 17, 0, time.UTC)
	short := webhookItem{row: store.WebhookSummaryRow{Project: "nuvion", Seq: 156, Method: "POST", Path: "/test", ReceivedAt: at, BodyLength: 747}}
	long := webhookItem{row: store.WebhookSummaryRow{
		Project: "nuvion", Seq: 155, Method: "POST",
		Path: "/a/much/longer/route/that/keeps/going/and/going/for/a/while", ReceivedAt: at, BodyLength: 738,
	}}

	const width = 110
	var a, b bytes.Buffer
	renderWebhookRow(&a, short, false, width, darkTheme())
	renderWebhookRow(&b, long, false, width, darkTheme())

	lineA, lineB := a.String(), b.String()
	if lipgloss.Width(lineA) != width+2 || lipgloss.Width(lineB) != width+2 {
		t.Fatalf("row widths = %d / %d, want %d", lipgloss.Width(lineA), lipgloss.Width(lineB), width+2)
	}
	// fmtTime renders local time, so compare against the formatter rather than a
	// literal clock reading.
	stamp := fmtTime(at)
	if !strings.HasSuffix(lineA, stamp) || !strings.HasSuffix(lineB, stamp) {
		t.Errorf("timestamp not flush right:\n%q\n%q", lineA, lineB)
	}
	if got := strings.Index(lineA, "747 B"); got != strings.Index(lineB, "738 B") {
		t.Errorf("payload column moved between rows (%d vs %d)", got, strings.Index(lineB, "738 B"))
	}
}

// TestListHeader_AlignsWithRows: the header is rendered through the same layout,
// so its labels must sit over the columns they name.
func TestListHeader_AlignsWithRows(t *testing.T) {
	t.Parallel()
	tbl := darkTheme()
	for _, tc := range []struct {
		name string
		tab  tab
		want []string
	}{
		{"ingress", tabIngress, []string{"METHOD", "SOURCE", "ROUTE", "PAYLOAD", "SEEN"}},
		{"traffic", tabTraffic, []string{"METHOD", "CODE", "URL", "TRANSFER", "SEEN"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			header := listHeader(tc.tab, 100, tbl)
			if lipgloss.Width(header) != 100 {
				t.Errorf("header width = %d, want 100", lipgloss.Width(header))
			}
			last := -1
			for _, label := range tc.want {
				idx := strings.Index(header, label)
				if idx < 0 {
					t.Fatalf("header missing %q: %q", label, header)
				}
				if idx <= last {
					t.Errorf("label %q out of order in %q", label, header)
				}
				last = idx
			}
			if !strings.Contains(header, "TRANSFER") && !strings.Contains(header, "PAYLOAD") {
				t.Errorf("header has no numeric column label: %q", header)
			}
		})
	}
}

// TestHasColumns: only the table tabs reserve a header line.
func TestHasColumns(t *testing.T) {
	t.Parallel()
	if !hasColumns(tabIngress) || !hasColumns(tabTraffic) {
		t.Error("ingress and traffic render as tables")
	}
	if hasColumns(tabTransforms) {
		t.Error("transforms is not a table")
	}
}

// TestListItemsAreSummaries: the lists must not carry payloads, which is what
// made the dashboard slow. This fails to compile if an item ever holds a full
// row again, and asserts the sizes the rows display survive the projection.
func TestListItemsAreSummaries(t *testing.T) {
	t.Parallel()
	items := captureItems([]store.TrafficCaptureSummaryRow{{
		ID: 7, Method: "GET", URL: "https://x.test/", Status: 200, ReqBodyLen: 11, RespBodyLen: 22,
	}}, "", "")
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	it, ok := items[0].(captureItem)
	if !ok {
		t.Fatalf("item type = %T", items[0])
	}
	if it.row.ReqBodyLen != 11 || it.row.RespBodyLen != 22 {
		t.Errorf("row = %+v, want the projected body lengths", it.row)
	}

	hooks := webhookItems([]store.WebhookSummaryRow{{
		Project: "p", Seq: 3, Method: "POST", Path: "/x", BodyLength: 747,
	}}, "")
	if len(hooks) != 1 {
		t.Fatalf("items = %d, want 1", len(hooks))
	}
	wh, ok := hooks[0].(webhookItem)
	if !ok {
		t.Fatalf("item type = %T", hooks[0])
	}
	if wh.row.BodyLength != 747 {
		t.Errorf("row = %+v, want the projected body length", wh.row)
	}
}
