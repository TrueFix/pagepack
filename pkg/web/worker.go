package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/TrueFix/pagepack/pkg/download"
	"github.com/TrueFix/pagepack/pkg/humanize"
	"github.com/TrueFix/pagepack/pkg/storage"
)

const (
	progressSaveEvery = time.Second
	progressLogEvery  = 3 * time.Second
)

func (s *Server) saveJob(job Job) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.state.Save(ctx, job)
}

// tracker owns a running job's state. Progress callbacks mutate it cheaply; a flush loop
// persists it at most once per second, and stage changes are saved immediately.
// saveMu keeps saves ordered so an older snapshot never overwrites a newer stage.
type tracker struct {
	s       *Server
	log     *slog.Logger
	mu      sync.Mutex
	saveMu  sync.Mutex
	job     Job
	dirty   bool
	lastLog time.Time
}

func (t *tracker) mutate(fn func(*Job)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fn(&t.job)
	t.job.UpdatedAt = time.Now().UTC()
	t.dirty = true
}

func (t *tracker) snapshot() Job {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.job
}

// commit saves the current state now.
func (t *tracker) commit() error {
	t.saveMu.Lock()
	defer t.saveMu.Unlock()
	t.mu.Lock()
	job := t.job
	t.dirty = false
	t.mu.Unlock()
	return t.s.saveJob(job)
}

// stage moves the job to a new stage and saves it.
func (t *tracker) stage(status Status) error {
	now := time.Now().UTC()
	t.mutate(func(j *Job) { j.Status, j.StageStartedAt = status, &now })
	if err := t.commit(); err != nil {
		return download.Fail(download.CodeUnavailable, "Job state storage is unavailable", err)
	}
	return nil
}

func (t *tracker) flushLoop(ctx context.Context) {
	ticker := time.NewTicker(progressSaveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			dirty := t.dirty
			t.mu.Unlock()
			if !dirty {
				continue
			}
			if err := t.commit(); err != nil {
				t.log.Warn("progress not saved", "err", err)
				t.mu.Lock()
				t.dirty = true // retry even if no new progress arrives
				t.mu.Unlock()
			}
			t.logProgress(t.snapshot())
		}
	}
}

func (t *tracker) logProgress(job Job) {
	if time.Since(t.lastLog) < progressLogEvery {
		return
	}
	t.lastLog = time.Now()
	p := job.Progress
	switch job.Status {
	case StatusCloning:
		t.log.Info("downloading", "downloaded", humanize.Bytes(p.DownloadedBytes), "files", p.Files, "rate", humanize.Rate(p.BytesPerSecond),
			"limit", fmt.Sprintf("%.0f%%", humanize.Percent(p.DownloadedBytes, t.s.config.MaxWebsiteBytes)), "failed_requests", p.RequestsFailed, "last", p.LastURL)
	case StatusZipping:
		t.log.Info("compressing", "files", fmt.Sprintf("%d/%d", p.ZippedFiles, p.Files), "progress", fmt.Sprintf("%.0f%%", humanize.Percent(p.ZippedBytes, p.DownloadedBytes)))
	case StatusUploading:
		t.log.Info("uploading", "sent", humanize.Bytes(p.UploadedBytes)+" / "+humanize.Bytes(p.ArchiveBytes), "progress", fmt.Sprintf("%.0f%%", humanize.Percent(p.UploadedBytes, p.ArchiveBytes)))
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (s *Server) worker(job Job, control *jobControl) {
	defer s.wg.Done()
	defer func() {
		control.cancel()
		s.activeMu.Lock()
		delete(s.active, job.ID)
		s.activeMu.Unlock()
		close(control.done)
	}()
	defer func() { <-s.slots }()
	t := &tracker{s: s, log: s.logger().With("job", shortID(job.ID)), job: job}

	flushCtx, stopFlush := context.WithCancel(context.Background())
	flushed := make(chan struct{})
	go func() { defer close(flushed); t.flushLoop(flushCtx) }()
	finish := func(err error) {
		stopFlush()
		<-flushed
		control.mu.Lock()
		requested := control.requested
		control.finalized = true
		control.mu.Unlock()
		if requested && download.Classify(err) != nil && download.Classify(err).Code != "cleanup_failed" {
			err = download.Fail(download.CodeCanceled, "Canceled by user", context.Canceled)
		}
		if err != nil {
			s.failJob(t, err)
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			finish(download.Fail(download.CodeInternal, "Unexpected processing error", fmt.Errorf("panic: %v", recovered)))
		}
	}()

	finish(s.run(control.ctx, t, control))
}

func (s *Server) run(ctx context.Context, t *tracker, control *jobControl) (runErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	job := t.snapshot()
	started := time.Now().UTC()
	t.mutate(func(j *Job) { j.StartedAt = &started })
	if err := t.stage(StatusCloning); err != nil {
		return err
	}
	t.log.Info("job started", "url", job.URL, "waited", started.Sub(job.CreatedAt))

	ws, err := download.NewWorkspace(s.config.TempDir)
	if err != nil {
		return download.Fail(download.CodeInternal, "Couldn't prepare scratch space", err)
	}
	workspaceCleaned := false
	defer func() {
		if workspaceCleaned {
			return
		}
		if err := ws.Cleanup(); err != nil {
			runErr = download.Fail("cleanup_failed", "Couldn't remove temporary job files", err)
			t.log.Error("temporary cleanup failed", "err", err)
		}
	}()

	// 1. Download
	applyMirror := func(st download.MirrorStats) {
		t.mutate(func(j *Job) {
			j.Progress.DownloadedBytes, j.Progress.Files = st.Bytes, st.Files
			j.Progress.RequestsOK, j.Progress.RequestsFailed = st.Fetched, st.FailedRequests
			j.Progress.LastURL, j.Progress.BytesPerSecond = st.LastURL, st.BytesPerSecond
		})
	}
	mirror, err := download.Mirror(ctx, ws, download.MirrorOptions{
		URL: job.URL, WgetPath: s.config.WgetPath, MaxBytes: s.config.MaxWebsiteBytes,
		Timeout: s.config.WgetTimeout, Tries: s.config.WgetTries,
		Log: t.log, OnProgress: applyMirror,
	})
	applyMirror(mirror.MirrorStats)
	if err != nil {
		return err
	}
	downloadDone := time.Now().UTC()
	t.log.Info("download finished", "size", humanize.Bytes(mirror.Bytes), "files", mirror.Files, "requests", mirror.Fetched,
		"failed_requests", mirror.FailedRequests, "took", mirror.Elapsed, "rate", humanize.Rate(mirror.BytesPerSecond))
	for _, warning := range mirror.Warnings {
		t.log.Warn("download warning", "warning", warning)
	}
	t.mutate(func(j *Job) { j.Warnings = mirror.Warnings })

	// 2. Compress
	if err := t.stage(StatusZipping); err != nil {
		return err
	}
	archive, err := download.Zip(ctx, ws.Site, ws.Archive, download.ZipOptions{
		TotalFiles: mirror.Files, TotalBytes: mirror.Bytes,
		OnProgress: func(z download.ZipStats) {
			t.mutate(func(j *Job) { j.Progress.ZippedFiles, j.Progress.ZippedBytes = z.Files, z.Bytes })
		},
	})
	if err != nil {
		return err
	}
	zipDone := time.Now().UTC()
	t.mutate(func(j *Job) { j.Progress.ArchiveBytes = archive.Bytes })
	t.log.Info("archive created", "archive", humanize.Bytes(archive.Bytes), "site", humanize.Bytes(mirror.Bytes),
		"saved", fmt.Sprintf("%.0f%%", 100-humanize.Percent(archive.Bytes, mirror.Bytes)), "files", archive.Files, "took", archive.Elapsed, "sha256", archive.SHA256[:12])

	// 3. Upload
	if err := t.stage(StatusUploading); err != nil {
		return err
	}
	key := strings.Trim(s.config.S3Prefix, "/") + "/" + job.ID + ".zip"
	stored := false
	// A canceled/failed PUT may have reached storage even if the client got an
	// error. Delete with an independent context after the upload has stopped.
	defer func() {
		if stored {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.objects.Delete(cleanupCtx, key); err != nil {
			runErr = download.Fail("cleanup_failed", "Couldn't remove the attempted archive upload", err)
			t.log.Error("archive cleanup failed", "key", key, "err", err)
		}
	}()
	filename := archiveName(job.Host(), job.CreatedAt)
	err = s.objects.Upload(ctx, key, archive.Path, storage.UploadOptions{
		DownloadName: filename,
		OnProgress:   func(sent, _ int64) { t.mutate(func(j *Job) { j.Progress.UploadedBytes = sent }) },
	})
	if err != nil {
		if ctx.Err() != nil {
			return download.Classify(ctx.Err())
		}
		return download.Fail(download.CodeUpload, "Couldn't upload the archive to storage", err)
	}
	finished := time.Now().UTC()
	t.log.Info("upload finished", "size", humanize.Bytes(archive.Bytes), "took", finished.Sub(zipDone),
		"rate", humanize.Rate(humanize.PerSecond(archive.Bytes, finished.Sub(zipDone))), "key", key)
	if err := ws.Cleanup(); err != nil {
		return download.Fail("cleanup_failed", "Couldn't remove temporary job files", err)
	}
	workspaceCleaned = true

	// 4. Done
	control.mu.Lock()
	defer control.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mutate(func(j *Job) {
		j.Status, j.FinishedAt, j.StageStartedAt, j.ObjectKey = StatusDone, &finished, &finished, key
		j.Progress.LastURL = ""
		j.Result = &Result{
			Filename: filename, ArchiveBytes: archive.Bytes, SiteBytes: mirror.Bytes, Files: mirror.Files,
			RequestsOK: mirror.Fetched, RequestsFailed: mirror.FailedRequests, SHA256: archive.SHA256,
			QueueMS: started.Sub(job.CreatedAt).Milliseconds(), DownloadMS: downloadDone.Sub(started).Milliseconds(),
			ZipMS: zipDone.Sub(downloadDone).Milliseconds(), UploadMS: finished.Sub(zipDone).Milliseconds(),
			TotalMS: finished.Sub(job.CreatedAt).Milliseconds(),
		}
	})
	if err := t.commit(); err != nil {
		return download.Fail(download.CodeUnavailable, "Job state storage is unavailable", err)
	}
	stored = true
	control.finalized = true
	t.log.Info("job done", "host", job.Host(), "archive", humanize.Bytes(archive.Bytes), "files", mirror.Files, "total", finished.Sub(job.CreatedAt))
	return nil
}

// userFacing failures are about the website or the request, not the server: logged as warnings.
var userFacing = map[string]bool{
	download.CodeTooLarge: true, download.CodeTimeout: true, download.CodeNetwork: true, download.CodeTLS: true,
	download.CodeAuth: true, download.CodeServer: true, download.CodeEmpty: true,
}

func (s *Server) failJob(t *tracker, err error) {
	e := download.Classify(err)
	if e.Code == download.CodeCanceled && s.ctx.Err() != nil {
		e.Message = "The server shut down before the job finished"
	}
	now := time.Now().UTC()
	var job Job
	t.mutate(func(j *Job) {
		j.Failure = &Failure{Code: e.Code, Message: e.Message, Stage: j.Status}
		j.Status, j.Message, j.FinishedAt = StatusError, e.Message, &now
		j.Result, j.ObjectKey = nil, ""
		if e.Code == download.CodeCanceled {
			j.Status = StatusCanceled
			j.Progress.BytesPerSecond = 0
		}
		job = *j
	})
	level := slog.LevelError
	if userFacing[e.Code] {
		level = slog.LevelWarn
	}
	attrs := []any{"stage", job.Failure.Stage, "code", e.Code, "reason", e.Message, "after", now.Sub(job.CreatedAt)}
	if cause := errors.Unwrap(e); cause != nil {
		attrs = append(attrs, "err", cause)
	}
	message := "job failed"
	if job.Status == StatusCanceled {
		message, level = "job canceled; cleanup complete", slog.LevelInfo
	}
	t.log.Log(context.Background(), level, message, attrs...)
	if saveErr := t.commit(); saveErr != nil {
		t.log.Error("failure not saved", "err", saveErr)
	}
}
