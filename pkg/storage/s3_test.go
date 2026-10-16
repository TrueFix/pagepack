package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TrueFix/pagepack/pkg/config"
)

func TestS3UploadPresignAndDelete(t *testing.T) {
	var uploaded, deleted bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/archives/clones/test.zip" {
			t.Errorf("unexpected object path: %s", r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256") {
			t.Error("request was not signed")
		}
		switch r.Method {
		case "PUT":
			if !strings.Contains(r.Header.Get("Content-Disposition"), `filename=example.com-2026-10-16.zip`) {
				t.Errorf("unexpected Content-Disposition: %s", r.Header.Get("Content-Disposition"))
			}
			data, _ := io.ReadAll(r.Body)
			if string(data) != "archive-data" {
				t.Errorf("unexpected upload: %s", data)
			}
			uploaded = true
		case "DELETE":
			deleted = true
		default:
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer endpoint.Close()
	c := config.Config{S3Bucket: "archives", S3Region: "us-east-1", S3Endpoint: endpoint.URL, S3ForcePathStyle: true, DownloadTTL: 15 * time.Minute, AWSAccessKeyID: "test-key", AWSSecretAccessKey: "test-secret"}
	s, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "archive.zip")
	if err := os.WriteFile(file, []byte("archive-data"), 0600); err != nil {
		t.Fatal(err)
	}
	var sent, total int64
	err = s.Upload(context.Background(), "clones/test.zip", file, UploadOptions{DownloadName: "example.com-2026-10-16.zip", OnProgress: func(n, of int64) { sent, total = n, of }})
	if err != nil {
		t.Fatal(err)
	}
	if sent != int64(len("archive-data")) || total != sent {
		t.Fatalf("upload progress %d/%d", sent, total)
	}
	link, expires, err := s.DownloadURL(context.Background(), "clones/test.zip")
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(expires); d < 14*time.Minute || d > 15*time.Minute {
		t.Fatalf("unexpected expiry in %s", d)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("X-Amz-Expires") != "900" || u.Query().Get("X-Amz-Signature") == "" {
		t.Fatalf("invalid presigned URL: %s", link)
	}
	if err := s.Delete(context.Background(), "clones/test.zip"); err != nil {
		t.Fatal(err)
	}
	if !uploaded || !deleted {
		t.Fatal("missing S3 operation")
	}
}
