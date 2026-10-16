package web

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/TrueFix/pagepack/pkg/humanize"
)

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(code int) {
	if r.status != 0 {
		return
	}
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// quiet requests are routine noise (static files, status polling) and only logged at debug
// level when they succeed; their failures are always visible.
func quiet(r *http.Request) bool {
	p := r.URL.Path
	return r.Method == http.MethodGet && (p == "/" || p == "/mark.svg" || strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/api/v1/jobs/") || p == "/api/v1/health") ||
		r.Method == http.MethodPost && p == "/status"
}

// logRequests records one line per request and turns handler panics into JSON 500s.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger().Error("handler panic", "method", r.Method, "path", r.URL.Path, "err", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				if rec.status == 0 {
					failure(rec, http.StatusInternalServerError, errInternal, "Unexpected server error")
				}
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400:
				level = slog.LevelWarn
			case quiet(r):
				level = slog.LevelDebug
			}
			s.logger().Log(r.Context(), level, r.Method+" "+r.URL.Path, "status", status, "took", time.Since(start), "size", humanize.Bytes(rec.bytes), "ip", clientIP(r))
		}()
		next.ServeHTTP(rec, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
