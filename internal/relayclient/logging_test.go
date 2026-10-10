package relayclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/plutack/wiretap/internal/logging"
	"github.com/plutack/wiretap/internal/relayproto"
	"github.com/plutack/wiretap/internal/store"
)

func TestTunnelDiagnosticPrivacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	logger, sink, err := logging.OpenFile(path, "json", "debug")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	conn := NewFakeConn()
	dialer := &FakeDialer{}
	dialer.Queue(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c := New(Config{URL: "ws://private-host/tunnel?secret=query-secret", ClientID: "id-secret", ClientToken: "token-secret"}, freshPCStore(t), WithLogger(logger), WithDialer(dialer), WithCallbacks(Callbacks{OnWebhook: func(store.WebhookRow) { cancel() }}))
	if err := conn.Send(relayproto.OK{Base: relayproto.Base{Type: relayproto.TypeOK}, Projects: []string{"project-secret"}}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Send(relayproto.Push{Base: relayproto.Base{Type: relayproto.TypePush}, Project: "project-secret", Seq: 1, Body: []byte("payload-secret"), Headers: map[string][]string{"Authorization": {"header-secret"}}}); err != nil {
		t.Fatal(err)
	}
	_ = c.Run(ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, event := range []string{"relay_connected", "webhook_processed", "relay_disconnected"} {
		if !strings.Contains(text, event) {
			t.Fatal("missing", event, text)
		}
	}
	for _, secret := range []string{"query-secret", "id-secret", "token-secret", "project-secret", "payload-secret", "header-secret", "private-host"} {
		if strings.Contains(text, secret) {
			t.Fatal("leaked", secret)
		}
	}
}
