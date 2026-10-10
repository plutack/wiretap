package relayd

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type requestLogKey struct{}

func (s *Server) requestLogger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(requestLogKey{}).(*slog.Logger); ok {
		return logger
	}
	return s.logger
}

// logRequests intentionally omits URL/path, query, headers, bodies and remote
// addresses. Client-provided correlation IDs are not trusted or echoed.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			s.logger.Error("request_id_failed")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		requestID := hex.EncodeToString(id[:])
		logger := s.logger.With("request_id", requestID)
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(context.WithValue(r.Context(), requestLogKey{}, logger))
		rw := &logResponseWriter{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(rw, r)
		status := rw.status
		if status == 0 {
			status = http.StatusOK
		}
		level := slog.LevelDebug
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		route := "ingress"
		if r.Pattern != "" && r.Pattern != "/" {
			route = r.Pattern
		}
		logger.Log(r.Context(), level, "http_request_completed", "route", route, "status", status, "duration", time.Since(start))
	})
}

// Preserve upgrade support: tunnel handlers require http.Hijacker. Unwrap also
// lets ResponseController reach deadlines and other underlying capabilities.
type logResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *logResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *logResponseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *logResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *logResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.status = http.StatusSwitchingProtocols
	}
	return conn, rw, err
}
func (w *logResponseWriter) FlushError() error {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
func (w *logResponseWriter) Flush() { _ = w.FlushError() }
