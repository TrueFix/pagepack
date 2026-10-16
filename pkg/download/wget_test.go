package download

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func needWget(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("wget"); err != nil {
		t.Skip("wget not installed")
	}
}

// testSite serves an index page that links a stylesheet and a missing image.
func testSite(t *testing.T, indexStatus int) *httptest.Server {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(indexStatus)
			_, _ = w.Write([]byte(`<html><head><link rel="stylesheet" href="/style.css"></head><body><img src="/missing.png">` + strings.Repeat("website ", 1000) + `</body></html>`))
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write([]byte("body{color:red}"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.Close)
	return site
}

func TestMirrorAndZipReportStats(t *testing.T) {
	needWget(t)
	site := testSite(t, http.StatusOK)
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mirror, err := Mirror(ctx, ws, MirrorOptions{URL: site.URL, WgetPath: "wget", MaxBytes: 1 << 20, Interval: 10 * time.Millisecond})
	if err != nil {
		t.Fatalf("Mirror: %v", err)
	}
	if mirror.Files < 2 || mirror.Bytes < 8000 || mirror.Fetched < 2 {
		t.Fatalf("unexpected stats: %+v", mirror.MirrorStats)
	}
	// The missing image is a partial failure: reported, but not fatal.
	if mirror.FailedRequests != 1 || len(mirror.Warnings) != 1 {
		t.Fatalf("expected one failed request and warning: %+v %v", mirror.MirrorStats, mirror.Warnings)
	}

	var updates int
	archive, err := Zip(ctx, ws.Site, ws.Archive, ZipOptions{TotalFiles: mirror.Files, TotalBytes: mirror.Bytes, OnProgress: func(ZipStats) { updates++ }})
	if err != nil {
		t.Fatalf("Zip: %v", err)
	}
	data, err := os.ReadFile(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if archive.Bytes != int64(len(data)) || archive.SHA256 != hex.EncodeToString(sum[:]) || archive.Files != mirror.Files || updates == 0 {
		t.Fatalf("archive metadata mismatch: %+v (size %d, updates %d)", archive, len(data), updates)
	}
	z, err := zip.OpenReader(archive.Path)
	if err != nil {
		t.Fatal(err)
	}
	_ = z.Close()

	ws.Cleanup()
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("scratch files not cleaned up: %v", entries)
	}
}

func TestMirrorClassifiesFailures(t *testing.T) {
	needWget(t)
	for _, tc := range []struct {
		name  string
		url   string
		limit int64
		code  string
	}{
		{"tooLarge", testSite(t, http.StatusOK).URL, 10, CodeTooLarge},
		{"serverError", testSite(t, http.StatusNotFound).URL, 1 << 20, CodeServer},
		{"unreachable", "http://127.0.0.1:1/", 1 << 20, CodeNetwork},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws, err := NewWorkspace(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err = Mirror(ctx, ws, MirrorOptions{URL: tc.url, WgetPath: "wget", MaxBytes: tc.limit, Interval: 10 * time.Millisecond})
			var e *Error
			if !errors.As(err, &e) || e.Code != tc.code {
				t.Fatalf("want code %s, got %v", tc.code, err)
			}
		})
	}
}

func TestMirrorMissingExecutable(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	if _, err := Mirror(context.Background(), ws, MirrorOptions{URL: "https://example.com", WgetPath: filepath.Join(ws.Dir, "missing-wget"), MaxBytes: 1024}); err == nil {
		t.Fatal("expected startup error")
	}
}

func TestZipPropagatesReadAndCancellationErrors(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "site")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	// A broken symlink forces a failure inside the walk callback rather than at its root.
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(source, "broken")); err != nil {
		t.Skip(err)
	}
	archive := filepath.Join(root, "archive.zip")
	var e *Error
	if _, err := Zip(context.Background(), source, archive, ZipOptions{}); !errors.As(err, &e) || e.Code != CodeArchive {
		t.Fatalf("expected archive error, got %v", err)
	}
	if _, err := os.Stat(archive); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("incomplete archive retained")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Zip(ctx, source, archive, ZipOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation: %v", err)
	}
}

func TestClassify(t *testing.T) {
	if Classify(context.DeadlineExceeded).Code != CodeTimeout || Classify(errors.New("x")).Code != CodeInternal || Classify(nil) != nil {
		t.Fatal("unexpected classification")
	}
	wrapped := Fail(CodeUpload, "upload", errors.New("boom"))
	if Classify(wrapped) != wrapped {
		t.Fatal("classified errors must pass through")
	}
}

func TestCancelDuringZipRemovesPartialArchive(t *testing.T) {
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	if err := os.WriteFile(filepath.Join(ws.Site, "large.html"), []byte(strings.Repeat("cancel me ", 100000)), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = Zip(ctx, ws.Site, ws.Archive, ZipOptions{OnProgress: func(ZipStats) { cancel() }})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected mid-compression cancellation, got %v", err)
	}
	if _, err := os.Stat(ws.Archive); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial archive was retained")
	}
}

func TestMirrorSkipsStalledLinksAndKeepsOtherPages(t *testing.T) {
	needWget(t)
	for _, partialBody := range []bool{false, true} {
		t.Run(map[bool]string{false: "stalled_headers", true: "stalled_body"}[partialBody], func(t *testing.T) {
			site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/":
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte(`<html><a href="/home.en.html?distro=parabola">slow</a><a href="/good.html">good</a></html>`))
				case "/home.en.html":
					if partialBody {
						w.Header().Set("Content-Type", "text/html")
						w.Header().Set("Content-Length", "100000")
						_, _ = w.Write([]byte("unfinished-page"))
						w.(http.Flusher).Flush()
					}
					<-r.Context().Done()
				case "/good.html":
					_, _ = w.Write([]byte("<html>completed-good-page</html>"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer site.Close()
			ws, err := NewWorkspace(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := Mirror(ctx, ws, MirrorOptions{URL: site.URL, WgetPath: "wget", MaxBytes: 1 << 20, Timeout: 200 * time.Millisecond})
			if err != nil {
				t.Fatalf("mirror should preserve completed pages: %v", err)
			}
			if result.Files != 2 || result.Fetched != 2 || result.FailedRequests < 1 || len(result.Warnings) == 0 {
				t.Fatalf("unexpected result: %+v", result)
			}
			archive, err := Zip(ctx, ws.Site, ws.Archive, ZipOptions{})
			if err != nil {
				t.Fatal(err)
			}
			z, err := zip.OpenReader(archive.Path)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			var good bool
			for _, file := range z.File {
				if strings.Contains(file.Name, "home.en") {
					t.Fatalf("stalled link retained in archive: %s", file.Name)
				}
				if strings.HasSuffix(file.Name, "good.html") {
					good = true
				}
			}
			if !good {
				t.Fatal("wget didn't continue to the healthy link")
			}
		})
	}
}

func TestMirrorStalledStartingPageFails(t *testing.T) {
	needWget(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer site.Close()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = Mirror(ctx, ws, MirrorOptions{URL: site.URL, WgetPath: "wget", MaxBytes: 1 << 20, Timeout: 200 * time.Millisecond})
	if e := Classify(err); e == nil || e.Code != CodeNetwork {
		t.Fatalf("expected network failure, got %v", err)
	}
}

func TestMirrorAllowsSlowButProgressingResponse(t *testing.T) {
	needWget(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		for i := 0; i < 6; i++ {
			_, _ = w.Write([]byte("<p>still downloading</p>"))
			w.(http.Flusher).Flush()
			time.Sleep(70 * time.Millisecond)
		}
	}))
	defer site.Close()
	ws, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Mirror(ctx, ws, MirrorOptions{URL: site.URL, WgetPath: "wget", MaxBytes: 1 << 20, Timeout: 250 * time.Millisecond})
	if err != nil || result.Files != 1 || len(result.Warnings) != 0 {
		t.Fatalf("healthy transfer should outlive the inactivity timeout: %+v %v", result, err)
	}
}
