package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/plutack/wiretap/internal/store"
)

// detailModel is the full-area detail pane, the TUI twin of the GUI's
// webhook-detail / traffic-detail overlay: summary line, headers, then the
// body rendered through the content-aware viewer. One viewport scrolls the
// whole document; `m` cycles the body mode, `tab` jumps between the request
// and response halves of a capture.
type detailModel struct {
	kind detailKind
	wh   *store.WebhookRow
	cap  *store.TrafficCaptureRow

	mode     bodyMode
	half     int // capture only: 0 = request, 1 = response
	respYOff int // y offset of the response section, for tab jumping

	vp         viewport.Model
	width      int
	height     int
	titleLine  string
	subtitleLn string
}

type detailKind int

const (
	detailWebhook detailKind = iota
	detailCapture
)

func newWebhookDetail(wh *store.WebhookRow, width, height int) detailModel {
	d := detailModel{kind: detailWebhook, wh: wh, mode: modeAuto}
	d.titleLine = fmt.Sprintf("%s %s/%d", wh.Method, wh.Project, wh.Seq)
	d.subtitleLn = fmt.Sprintf("%s · %s · %s",
		wh.Path, byteCount(len(wh.Body)), wh.ReceivedAt.Local().Format("2006-01-02 15:04:05"))
	d.resize(width, height)
	return d
}

func newCaptureDetail(c *store.TrafficCaptureRow, width, height int) detailModel {
	d := detailModel{kind: detailCapture, cap: c, mode: modeAuto}
	d.titleLine = fmt.Sprintf("%s %s", c.Method, c.URL)
	d.subtitleLn = fmt.Sprintf("captured %s · session %d",
		c.At.Local().Format("2006-01-02 15:04:05"), c.SessionID)
	d.resize(width, height)
	return d
}

func (d detailModel) selectedWebhook() *store.WebhookRow {
	if d.kind == detailWebhook {
		return d.wh
	}
	return nil
}

func (d detailModel) selectedCapture() *store.TrafficCaptureRow {
	if d.kind == detailCapture {
		return d.cap
	}
	return nil
}

// resize re-lays out the pane and rebuilds the document, preserving the
// viewport offset when the body mode (not the size) changed.
func (d *detailModel) resize(width, height int) {
	d.width, d.height = width, height
	head := 2 // title + subtitle
	d.vp = viewport.New(maxInt(width, 10), maxInt(height-head-1, 3))
	doc := d.build()
	d.vp.SetContent(doc)
	d.clampJump()
}

func (d *detailModel) clampJump() {
	if d.respYOff > d.vp.YOffset+d.vp.Height {
		d.respYOff = d.vp.YOffset + d.vp.Height
	}
}

// build renders the whole scrolled document: summary + headers + body
// (webhook), or request half then response half (capture).
func (d *detailModel) build() string {
	var b strings.Builder
	switch d.kind {
	case detailWebhook:
		d.renderHeaderSection(&b, "Headers", headerLines(d.wh.RawHeaders, d.wh.HeadersJSON))
		d.renderBodySection(&b, "Body", d.wh.Body, contentHeaderValue(headerParse(d.wh.HeadersJSON)))
	case detailCapture:
		d.renderHeaderSection(&b, "Request headers", headerLines(nil, d.cap.ReqHeadersJSON))
		d.renderBodySection(&b, "Request body", d.cap.ReqBody, contentHeaderValue(headerParse(d.cap.ReqHeadersJSON)))
		d.respYOff = strings.Count(b.String(), "\n") + 1
		status := "–"
		if d.cap.Status != 0 {
			status = fmt.Sprintf("%d %s", d.cap.Status, http.StatusText(d.cap.Status))
		}
		fmt.Fprintf(&b, "\n%s\n\n", currentTheme.accent.Render("── Response · "+status+" ──"))
		d.renderHeaderSection(&b, "Response headers", headerLines(nil, d.cap.RespHeadersJSON))
		d.renderBodySection(&b, "Response body", d.cap.RespBody, contentHeaderValue(headerParse(d.cap.RespHeadersJSON)))
	}
	return b.String()
}

func (d *detailModel) renderHeaderSection(b *strings.Builder, title string, lines []string) {
	fmt.Fprintf(b, "\n%s\n\n", currentTheme.accent.Render("── "+title+" ──"))
	if len(lines) == 0 {
		fmt.Fprintf(b, "%s\n", currentTheme.dim.Render("(none)"))
		return
	}
	for _, ln := range lines {
		fmt.Fprintln(b, ln)
	}
}

func (d *detailModel) renderBodySection(b *strings.Builder, title string, body []byte, contentType string) {
	fmt.Fprintf(b, "\n%s\n\n", currentTheme.accent.Render(
		fmt.Sprintf("── %s · %s mode · %s ──", title, d.mode, byteCount(len(body)))))
	if len(body) == 0 {
		fmt.Fprintf(b, "%s\n", currentTheme.dim.Render("(empty)"))
		return
	}
	b.WriteString(renderBody(body, contentType, d.mode))
	b.WriteByte('\n')
}

func (d detailModel) Update(msg tea.Msg) (detailModel, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "m":
			d.mode = d.mode.next()
			d.rebuild()
		case "tab", "shift+tab":
			if d.kind == detailCapture {
				if d.half == 0 {
					d.half = 1
					d.vp.SetYOffset(d.respYOff)
				} else {
					d.half = 0
					d.vp.GotoTop()
				}
				return d, nil
			}
		}
	}
	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return d, cmd
}

// rebuild re-renders the document after a body-mode switch without losing
// the reader's position.
func (d *detailModel) rebuild() {
	y := d.vp.YOffset
	d.vp.SetContent(d.build())
	d.vp.SetYOffset(y)
}

func (d detailModel) View() string {
	var b strings.Builder
	b.WriteString(currentTheme.title.Render(cutWidth(d.titleLine, maxInt(d.width-2, 10))))
	b.WriteByte('\n')
	b.WriteString(currentTheme.dim.Render(cutWidth(d.subtitleLn, maxInt(d.width-2, 10))))
	b.WriteByte('\n')
	b.WriteString(d.vp.View())
	b.WriteByte('\n')
	hint := "esc back · j/k scroll · m body:" + d.mode.String()
	if d.kind == detailCapture {
		hint += " · tab request/response"
	} else {
		hint += " · r replay · e export · y copy body"
	}
	b.WriteString(currentTheme.dim.Render(hint))
	return b.String()
}

// --- header helpers -------------------------------------------------------

// headerLines renders a header block. The raw block (webhooks keep it
// byte-exact, order and duplicates preserved) is preferred; parsed JSON is
// the fallback, alphabetized because map order is not stable.
func headerLines(raw []byte, headersJSON string) []string {
	if len(raw) > 0 {
		out := strings.Split(strings.TrimRight(string(raw), "\r\n"), "\n")
		for i, ln := range out {
			out[i] = strings.TrimRight(ln, "\r")
		}
		return out
	}
	h := headerParse(headersJSON)
	if len(h) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		for _, v := range h[k] {
			out = append(out, k+": "+v)
		}
	}
	return out
}

// headerParse decodes the JSON-encoded http.Header the store keeps.
func headerParse(headersJSON string) http.Header {
	if headersJSON == "" {
		return http.Header{}
	}
	h := http.Header{}
	if err := json.Unmarshal([]byte(headersJSON), &h); err != nil {
		return http.Header{}
	}
	return h
}

// contentHeaderValue picks the Content-Type of a parsed header block, used
// as the auto-mode hint for the body viewer. http.Header.Get is canonical-
// key based, so this matches any casing.
func contentHeaderValue(h http.Header) string {
	return h.Get("Content-Type")
}
