package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/TrueFix/pagepack/pkg/config"
	"github.com/TrueFix/pagepack/pkg/storage"
)

type cancelObjects struct {
	fakeObjects
	entered   chan struct{}
	deleted   chan struct{}
	deleteErr error
}

func (f *cancelObjects) Upload(ctx context.Context, _, _ string, _ storage.UploadOptions) error {
	close(f.entered)
	<-ctx.Done()
	// Simulate the server accepting a PUT just as the client cancels it.
	return nil
}

func (f *cancelObjects) Delete(ctx context.Context, _ string) error {
	if ctx.Err() != nil {
		return errors.New("cleanup inherited canceled context")
	}
	close(f.deleted)
	return f.deleteErr
}

func waitFor(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker")
	}
}

func TestCancelStopsWorkerAndCleansFiles(t *testing.T) {
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget not installed")
	}
	for _, phase := range []string{"pending", "download", "upload", "cleanup_failure"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				if phase == "download" {
					_, _ = w.Write([]byte("<html>"))
					w.(http.Flusher).Flush()
					entered <- struct{}{}
					<-r.Context().Done()
					return
				}
				_, _ = w.Write([]byte("<html>hello</html>"))
			}))
			defer site.Close()
			state := &memoryState{}
			objects := &cancelObjects{entered: make(chan struct{}), deleted: make(chan struct{})}
			if phase == "cleanup_failure" {
				objects.deleteErr = errors.New("storage offline")
			}
			root := t.TempDir()
			s := &Server{config: config.Config{JobTimeout: 20 * time.Second, MaxWebsiteBytes: 1 << 20, TempDir: root, WgetPath: "wget", S3Prefix: "test"},
				state: state, objects: objects, slots: make(chan struct{}, 1), ctx: context.Background(), log: quietLogger()}
			if phase == "pending" {
				job := Job{ID: testID, URL: site.URL, Status: StatusPending, CreatedAt: time.Now()}
				_ = state.Save(context.Background(), job)
				s.slots <- struct{}{}
				control := s.registerJob(testID)
				// Endpoint requests cancellation before the worker has started.
				response := httptest.NewRecorder()
				returned := make(chan struct{})
				go func() {
					defer close(returned)
					s.Router().ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/jobs/"+testID+"/cancel", nil))
				}()
				waitFor(t, control.ctx.Done())
				s.wg.Add(1)
				go s.worker(job, control)
				waitFor(t, returned)
				if response.Code != 200 {
					t.Fatalf("cancel: %d %s", response.Code, response.Body.String())
				}
			} else {
				_, created := call(t, s, "POST", "/api/v1/jobs", `{"url":"`+site.URL+`"}`)
				id := created["request_id"].(string)
				if phase == "download" {
					waitFor(t, entered)
				} else {
					waitFor(t, objects.entered)
				}
				w, data := call(t, s, "POST", "/api/v1/jobs/"+id+"/cancel", "")
				want := "canceled"
				if phase == "cleanup_failure" {
					want = "error"
				}
				if w.Code != 200 || data["status"] != want {
					t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
				}
				if phase != "download" {
					waitFor(t, objects.deleted)
				}
				if phase == "cleanup_failure" {
					if data["error"].(map[string]any)["code"] != "cleanup_failed" {
						t.Fatalf("failure: %v", data)
					}
				} else {
					w, repeated := call(t, s, "POST", "/api/v1/jobs/"+id+"/cancel", "")
					if w.Code != 200 || repeated["status"] != "canceled" {
						t.Fatal("cancel must be idempotent")
					}
				}
			}
			s.wg.Wait()
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 || len(s.slots) != 0 || len(s.active) != 0 {
				t.Fatalf("resources leaked: files=%d workers=%d controls=%d", len(entries), len(s.slots), len(s.active))
			}
		})
	}
}

func TestCancelRejectsFinishedAndUnknownJobs(t *testing.T) {
	state := &memoryState{}
	_ = state.Save(context.Background(), doneJob())
	s := &Server{state: state, log: quietLogger()}
	for _, tc := range []struct {
		id   string
		code int
	}{{testID, 409}, {"bad", 400}, {"00000000-0000-0000-0000-000000000000", 404}} {
		w, _ := call(t, s, http.MethodPost, "/api/v1/jobs/"+tc.id+"/cancel", "")
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.id, w.Code, w.Body.String())
		}
	}
	if data, _ := state.Get(context.Background(), testID); data.Status != StatusDone || !strings.Contains(data.ObjectKey, "private") {
		t.Fatal("finished archive changed")
	}
}
