package proxy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestStopDrainsActiveCONNECTExchange(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	upstream, roots := startTLSUpstream(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("completed"))
	}))
	rec := &captureCollector{}
	p, pool := startProxyInTestForUpstream(t, rec, roots)
	proxyURL, _ := url.Parse("http://" + p.Addr())
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: &tls.Config{RootCAs: pool}}
	defer tr.CloseIdleConnections()
	done := make(chan error, 1)
	go func() {
		resp, err := (&http.Client{Transport: tr, Timeout: time.Second}).Get(upstream.URL)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			err = readErr
			if string(body) != "completed" {
				err = errors.New("incomplete CONNECT response")
			}
		}
		done <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- p.Stop(ctx) }()
	for !p.isDraining() {
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if len(rec.List()) != 1 {
		t.Fatal("lost capture during CONNECT drain")
	}
}

func TestStopDrainsAcceptedHTTPExchange(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(201)
		_, _ = w.Write([]byte("accepted"))
	}))
	defer upstream.Close()
	p := New("127.0.0.1:0", nil, &captureCollector{})
	if _, err := p.StartAsync(); err != nil {
		t.Fatal(err)
	}
	proxyURL, _ := url.Parse("http://" + p.Addr())
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: time.Second}
	done := make(chan error, 1)
	go func() {
		resp, err := client.Get(upstream.URL)
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 201 {
				err = errors.New("unexpected response")
			}
		}
		done <- err
	}()
	<-entered
	stopped := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { stopped <- p.Stop(ctx) }()
	for !p.isDraining() {
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if err := p.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStopForceCancelsUpstreamAndJoinsRecorderUsers(t *testing.T) {
	entered := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer upstream.Close()
	p := New("127.0.0.1:0", nil, &captureCollector{})
	if _, err := p.StartAsync(); err != nil {
		t.Fatal(err)
	}
	proxyURL, _ := url.Parse("http://" + p.Addr())
	tr := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer tr.CloseIdleConnections()
	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, _ := (&http.Client{Transport: tr, Timeout: time.Second}).Get(upstream.URL)
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	joinCtx, joinCancel := context.WithTimeout(context.Background(), time.Second)
	defer joinCancel()
	if err := p.Wait(joinCtx); err != nil {
		t.Fatal(err)
	}
	<-done
}
