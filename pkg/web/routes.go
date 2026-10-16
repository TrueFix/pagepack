package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/TrueFix/pagepack/pkg/humanize"
	"github.com/TrueFix/pagepack/pkg/logx"
	"github.com/TrueFix/pagepack/pkg/version"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/valkey-io/valkey-go"
)

const apiVersion = "v1"

// Error codes for request-level failures (job failures use download.Code*).
const (
	errInvalidJSON  = "invalid_json"
	errInvalidURL   = "invalid_url"
	errInvalidID    = "invalid_id"
	errBusy         = "busy"
	errNotFound     = "not_found"
	errMethod       = "method_not_allowed"
	errUnavailable  = "state_unavailable"
	errLinkFailed   = "download_link_unavailable"
	errInternal     = "internal_error"
	busyRetrySecond = 5
)

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// apiError is the single error shape for every endpoint. `msg` duplicates `error`
// for clients written against the original API.
type apiError struct {
	Status     string `json:"status"`
	Code       string `json:"code"`
	Error      string `json:"error"`
	Msg        string `json:"msg"`
	RetryAfter int    `json:"retry_after_seconds,omitempty"`
}

func failure(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, apiError{Status: "error", Code: code, Error: message, Msg: message})
}

func decode(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON object")
	}
	return nil
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	page, err := fs.ReadFile(s.frontend, "index.html")
	if err != nil {
		http.Error(w, "Frontend unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(page)
}

// GET /api/v1/config
func (s *Server) publicConfig(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]any{
		"api_version":          apiVersion,
		"version":              version.Version,
		"max_website_bytes":    s.config.MaxWebsiteBytes,
		"max_website_size":     humanize.Bytes(s.config.MaxWebsiteBytes),
		"max_concurrent_jobs":  s.config.MaxConcurrentJobs,
		"job_timeout_seconds":  int64(s.config.JobTimeout.Seconds()),
		"wget_timeout_seconds": s.config.WgetTimeout.Seconds(),
		"wget_tries":           s.config.WgetTries,
		"state_ttl_seconds":    int64(s.config.StateTTL.Seconds()),
		"download_ttl_seconds": int64(s.config.DownloadTTL.Seconds()),
		"stages":               pipeline,
	})
}

// GET /api/v1/health
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	stateStatus, status, code := "ok", "ok", http.StatusOK
	if s.state == nil || s.state.Ping(ctx) != nil {
		stateStatus, status, code = "unavailable", "degraded", http.StatusServiceUnavailable
	}
	uptime := time.Duration(0)
	if !s.started.IsZero() {
		uptime = time.Since(s.started)
	}
	busy := len(s.slots)
	respond(w, code, map[string]any{
		"status":         status,
		"api_version":    apiVersion,
		"version":        version.Version,
		"uptime_seconds": int64(uptime.Seconds()),
		"uptime":         logx.Duration(uptime),
		"workers":        map[string]int{"busy": busy, "available": cap(s.slots) - busy, "capacity": cap(s.slots)},
		"job_state":      stateStatus,
	})
}

// POST /api/v1/jobs  {"url": "https://example.com"}
func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		URL string `json:"url"`
	}
	if err := decode(w, r, &payload); err != nil {
		failure(w, http.StatusBadRequest, errInvalidJSON, "Send one JSON object like {\"url\": \"https://example.com\"}")
		return
	}
	u, err := url.Parse(payload.URL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		failure(w, http.StatusBadRequest, errInvalidURL, "Enter a valid HTTP or HTTPS website URL without credentials")
		return
	}
	select {
	case s.slots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", strconv.Itoa(busyRetrySecond))
		respond(w, http.StatusTooManyRequests, apiError{Status: "error", Code: errBusy, Error: "All workers are busy. Please try again shortly.", Msg: "All workers are busy. Please try again shortly.", RetryAfter: busyRetrySecond})
		s.logger().Warn("job rejected: workers busy", "url", u.String(), "capacity", cap(s.slots))
		return
	}
	now := time.Now().UTC()
	job := Job{ID: uuid.NewString(), URL: u.String(), Status: StatusPending, CreatedAt: now, UpdatedAt: now}
	if err := s.state.Save(r.Context(), job); err != nil {
		<-s.slots
		s.logger().Error("job not created: state unavailable", "url", job.URL, "err", err)
		failure(w, http.StatusServiceUnavailable, errUnavailable, "Job state is unavailable")
		return
	}
	s.logger().Info("job queued", "job", shortID(job.ID), "url", job.URL, "workers", strconv.Itoa(len(s.slots))+"/"+strconv.Itoa(cap(s.slots)))
	s.wg.Add(1)
	control := s.registerJob(job.ID)
	go s.worker(job, control)
	w.Header().Set("Location", "/api/v1/jobs/"+job.ID)
	respond(w, http.StatusAccepted, job.View(s.config.MaxWebsiteBytes, nil, now))
}

// GET /api/v1/jobs/{id}
func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	s.writeJob(w, r, mux.Vars(r)["id"])
}

// POST /status {"request_id": "..."} — original endpoint, same response as GET /api/v1/jobs/{id}.
func (s *Server) legacyStatus(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID string `json:"request_id"`
	}
	if err := decode(w, r, &payload); err != nil {
		failure(w, http.StatusBadRequest, errInvalidJSON, "Send one JSON object like {\"request_id\": \"…\"}")
		return
	}
	s.writeJob(w, r, payload.ID)
}

func (s *Server) writeJob(w http.ResponseWriter, r *http.Request, id string) {
	if _, err := uuid.Parse(id); err != nil {
		failure(w, http.StatusBadRequest, errInvalidID, "Invalid request ID")
		return
	}
	job, err := s.state.Get(r.Context(), id)
	if valkey.IsValkeyNil(err) {
		failure(w, http.StatusNotFound, errNotFound, "Job not found or expired")
		return
	}
	if err != nil {
		s.logger().Error("job state read failed", "job", shortID(id), "err", err)
		failure(w, http.StatusServiceUnavailable, errUnavailable, "Job state is unavailable")
		return
	}
	var dl *link
	if job.Status == StatusDone {
		u, expires, err := s.objects.DownloadURL(r.Context(), job.ObjectKey)
		if err != nil {
			s.logger().Error("download link failed", "job", shortID(id), "err", err)
			failure(w, http.StatusServiceUnavailable, errLinkFailed, "Download link is temporarily unavailable")
			return
		}
		dl = &link{url: u, expires: expires}
	}
	respond(w, http.StatusOK, job.View(s.config.MaxWebsiteBytes, dl, time.Now().UTC()))
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	failure(w, http.StatusNotFound, errNotFound, "No such endpoint: "+r.Method+" "+r.URL.Path)
}

func (s *Server) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	failure(w, http.StatusMethodNotAllowed, errMethod, r.Method+" is not supported for "+r.URL.Path)
}
