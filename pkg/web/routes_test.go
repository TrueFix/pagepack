package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TrueFix/pagepack/pkg/config"
	"github.com/TrueFix/pagepack/pkg/storage"
	"github.com/valkey-io/valkey-go"
)

const testID = "3d809ef3-acd2-4904-8626-c6201e60ab49"

// memoryState records every saved snapshot so tests can follow a job's stages.
type memoryState struct {
	mu      sync.Mutex
	jobs    map[string]Job
	history []Status
	err     error
}

func (m *memoryState) Save(_ context.Context, job Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	if m.jobs == nil {
		m.jobs = map[string]Job{}
	}
	m.jobs[job.ID] = job
	if n := len(m.history); n == 0 || m.history[n-1] != job.Status {
		m.history = append(m.history, job.Status)
	}
	return nil
}

func (m *memoryState) Get(_ context.Context, id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return Job{}, m.err
	}
	job, ok := m.jobs[id]
	if !ok {
		return Job{}, valkey.Nil
	}
	return job, nil
}

func (m *memoryState) Ping(context.Context) error { return m.err }

type fakeObjects struct {
	mu       sync.Mutex
	uploaded map[string]int64
	names    map[string]string
}

func (f *fakeObjects) Upload(_ context.Context, key, filename string, o storage.UploadOptions) error {
	info, err := os.Stat(filename)
	if err != nil {
		return err
	}
	if o.OnProgress != nil {
		o.OnProgress(info.Size(), info.Size())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.uploaded == nil {
		f.uploaded, f.names = map[string]int64{}, map[string]string{}
	}
	f.uploaded[key], f.names[key] = info.Size(), o.DownloadName
	return nil
}

func (f *fakeObjects) DownloadURL(context.Context, string) (string, time.Time, error) {
	return "https://storage.example/archive.zip", time.Now().Add(15 * time.Minute), nil
}
func (f *fakeObjects) Delete(context.Context, string) error { return nil }

func quietLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func call(t *testing.T, s *Server, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, httptest.NewRequest(method, path, reader))
	var data map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil && w.Header().Get("Content-Type") == "application/json" {
		t.Fatalf("invalid JSON: %s", w.Body.String())
	}
	return w, data
}

func TestCreateJobRejectsBadInputBusyAndStateFailure(t *testing.T) {
	for _, path := range []string{"/api/v1/jobs", "/clone"} {
		for _, tc := range []struct {
			name, body, code   string
			busy, stateFailure bool
			status             int
		}{
			{"invalidJSON", "{", errInvalidJSON, false, false, 400},
			{"optionInjection", `{"url":"--help"}`, errInvalidURL, false, false, 400},
			{"invalidScheme", `{"url":"file:///etc/passwd"}`, errInvalidURL, false, false, 400},
			{"extraJSON", `{"url":"https://example.com"} {}`, errInvalidJSON, false, false, 400},
			{"busy", `{"url":"https://example.com"}`, errBusy, true, false, 429},
			{"stateFailure", `{"url":"https://example.com"}`, errUnavailable, false, true, 503},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				state := &memoryState{}
				if tc.stateFailure {
					state.err = errors.New("offline")
				}
				s := &Server{state: state, slots: make(chan struct{}, 1), log: quietLogger()}
				if tc.busy {
					s.slots <- struct{}{}
				}
				w, data := call(t, s, http.MethodPost, path, tc.body)
				if w.Code != tc.status || data["code"] != tc.code || data["status"] != "error" || data["msg"] == "" {
					t.Fatalf("status %d; body %s", w.Code, w.Body.String())
				}
				if tc.busy && (w.Header().Get("Retry-After") == "" || data["retry_after_seconds"] == nil) {
					t.Fatal("busy response must say when to retry")
				}
				if tc.stateFailure && len(s.slots) != 0 {
					t.Fatal("worker slot leaked")
				}
			})
		}
	}
}

func doneJob() Job {
	created := time.Now().Add(-10 * time.Second).UTC()
	finished := created.Add(8 * time.Second)
	return Job{
		ID: testID, URL: "https://example.com/docs", Status: StatusDone, ObjectKey: "private-key",
		CreatedAt: created, FinishedAt: &finished,
		Result: &Result{Filename: "example.com-2026-10-16.zip", ArchiveBytes: 1 << 20, SiteBytes: 4 << 20, Files: 120, RequestsOK: 121, SHA256: "abc", DownloadMS: 6000, TotalMS: 8000},
	}
}

func TestGetJobReturnsResultAndDownloadWithoutPrivateKey(t *testing.T) {
	state := &memoryState{}
	_ = state.Save(context.Background(), doneJob())
	s := &Server{state: state, objects: &fakeObjects{}, log: quietLogger(), config: config.Config{MaxWebsiteBytes: 100 << 20}}

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/jobs/" + testID, ""},
		{http.MethodPost, "/status", `{"request_id":"` + testID + `"}`},
	} {
		w, data := call(t, s, tc.method, tc.path, tc.body)
		if w.Code != 200 || strings.Contains(w.Body.String(), "private-key") {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		result := data["result"].(map[string]any)
		download := data["download"].(map[string]any)
		if result["archive_size"] != "1 MB" || result["site_size"] != "4 MB" || result["space_saved_percent"] != 75.0 || result["files"] != 120.0 {
			t.Fatalf("result: %v", result)
		}
		if download["url"] == "" || download["filename"] != "example.com-2026-10-16.zip" || download["expires_in_seconds"].(float64) < 800 {
			t.Fatalf("download: %v", download)
		}
		if data["download_url"] == "" || data["host"] != "example.com" || !strings.HasPrefix(data["summary"].(string), "Ready: 1 MB ZIP") {
			t.Fatalf("job: %v", data)
		}
		if stage := data["stage"].(map[string]any); stage["step"] != 5.0 || stage["label"] != "Ready" {
			t.Fatalf("stage: %v", stage)
		}
	}
}

func TestJobViewDescribesProgress(t *testing.T) {
	j := Job{ID: testID, URL: "https://example.com", Status: StatusUploading, CreatedAt: time.Now(),
		Progress: Progress{DownloadedBytes: 10 << 20, Files: 40, ArchiveBytes: 4 << 20, UploadedBytes: 1 << 20}}
	v := j.View(100<<20, nil, time.Now())
	if v.Progress == nil || v.Progress.StagePercent == nil || *v.Progress.StagePercent != 25 || v.Progress.LimitPercent != 10 || v.Progress.Uploaded != "1 MB" {
		t.Fatalf("progress: %+v", v.Progress)
	}
	if v.Summary != "Uploading: 1 MB of 4 MB (25%)" || v.Stage.Step != 4 {
		t.Fatalf("summary %q step %d", v.Summary, v.Stage.Step)
	}
	failed := Job{ID: testID, Status: StatusError, Failure: &Failure{Code: "too_large", Message: "Too big", Stage: StatusCloning}}
	if fv := failed.View(1, nil, time.Now()); fv.Stage.Step != 2 || fv.Summary != "Too big" || fv.Error.Code != "too_large" {
		t.Fatalf("failed view: %+v", fv)
	}
}

func TestQueuedCountersAndLegacyArchiveDownload(t *testing.T) {
	now := time.Now().UTC()
	queued := Job{ID: testID, Status: StatusPending, CreatedAt: now}
	v := queued.View(100<<20, nil, now)
	if v.Progress == nil || v.Progress.DownloadedBy != 0 || v.Progress.LimitBytes != 100<<20 {
		t.Fatalf("queued counters: %+v", v.Progress)
	}
	// Completed entries from older builds have an object key but no Result.
	state := &memoryState{}
	_ = state.Save(context.Background(), Job{ID: testID, URL: "https://example.com", Status: StatusDone, ObjectKey: "legacy-private-key", CreatedAt: now})
	s := &Server{state: state, objects: &fakeObjects{}, log: quietLogger()}
	w, data := call(t, s, http.MethodGet, "/api/v1/jobs/"+testID, "")
	if w.Code != http.StatusOK || data["download_url"] == nil || data["download"] == nil || strings.Contains(w.Body.String(), "legacy-private-key") {
		t.Fatalf("legacy download: %d %s", w.Code, w.Body.String())
	}
}

func TestErrorsForMissingJobsAndRoutes(t *testing.T) {
	s := &Server{state: &memoryState{}, log: quietLogger()}
	for _, tc := range []struct {
		method, path, code string
		status             int
	}{
		{http.MethodGet, "/api/v1/jobs/" + testID, errNotFound, 404},
		{http.MethodGet, "/api/v1/jobs/not-a-uuid", errInvalidID, 400},
		{http.MethodGet, "/api/v1/nope", errNotFound, 404},
		{http.MethodDelete, "/api/v1/jobs", errMethod, 405},
	} {
		w, data := call(t, s, tc.method, tc.path, "")
		if w.Code != tc.status || data["code"] != tc.code {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestPublicConfigAndHealth(t *testing.T) {
	s := &Server{config: config.Config{MaxWebsiteBytes: 1234, ValkeyURL: "secret", S3Bucket: "private"}, state: &memoryState{}, slots: make(chan struct{}, 4), log: quietLogger(), started: time.Now()}
	s.slots <- struct{}{}
	w, _ := call(t, s, http.MethodGet, "/api/v1/config", "")
	if !strings.Contains(w.Body.String(), "1234") || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("response: %s", w.Body.String())
	}
	if legacy, _ := call(t, s, http.MethodGet, "/api/config", ""); legacy.Body.String() != w.Body.String() {
		t.Fatal("legacy config route differs")
	}
	w, data := call(t, s, http.MethodGet, "/api/v1/health", "")
	workers := data["workers"].(map[string]any)
	if w.Code != 200 || workers["busy"] != 1.0 || workers["available"] != 3.0 {
		t.Fatalf("health: %s", w.Body.String())
	}
	s.state = &memoryState{err: errors.New("down")}
	if w, data := call(t, s, http.MethodGet, "/api/v1/health", ""); w.Code != 503 || data["status"] != "degraded" {
		t.Fatalf("degraded health: %d %s", w.Code, w.Body.String())
	}
}

func TestWorkerRunsFullPipeline(t *testing.T) {
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget not installed")
	}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("<html><body>" + strings.Repeat("archive me ", 2000) + "</body></html>"))
	}))
	defer site.Close()
	state, objects := &memoryState{}, &fakeObjects{}
	s := &Server{
		config: config.Config{MaxWebsiteBytes: 1 << 20, JobTimeout: 20 * time.Second, TempDir: t.TempDir(), WgetPath: "wget", S3Prefix: "clones/"},
		state:  state, objects: objects, slots: make(chan struct{}, 1), ctx: context.Background(), log: quietLogger(),
	}
	w, data := call(t, s, http.MethodPost, "/api/v1/jobs", `{"url":"`+site.URL+`"}`)
	if w.Code != http.StatusAccepted || data["status"] != "pending" || w.Header().Get("Location") == "" {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	s.wg.Wait()

	job, _ := state.Get(context.Background(), data["request_id"].(string))
	if job.Status != StatusDone || job.Result == nil || job.Result.SiteBytes < 20000 || job.Result.ArchiveBytes == 0 || job.Result.SHA256 == "" {
		t.Fatalf("job: %+v result %+v failure %+v", job, job.Result, job.Failure)
	}
	if got := objects.uploaded[job.ObjectKey]; got != job.Result.ArchiveBytes || !strings.HasSuffix(objects.names[job.ObjectKey], ".zip") {
		t.Fatalf("uploaded %d bytes as %q, result says %d", got, objects.names[job.ObjectKey], job.Result.ArchiveBytes)
	}
	want := []Status{StatusPending, StatusCloning, StatusZipping, StatusUploading, StatusDone}
	if strings.Join(statusStrings(state.history), ",") != strings.Join(statusStrings(want), ",") {
		t.Fatalf("stages %v, want %v", state.history, want)
	}
	if len(s.slots) != 0 {
		t.Fatal("worker slot not released")
	}
}

func statusStrings(list []Status) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = string(s)
	}
	return out
}
