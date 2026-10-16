package web

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/TrueFix/pagepack/pkg/humanize"
	"github.com/TrueFix/pagepack/pkg/logx"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusCloning   Status = "cloning"
	StatusZipping   Status = "zipping"
	StatusUploading Status = "uploading"
	StatusDone      Status = "done"
	StatusError     Status = "error"
	StatusCanceled  Status = "canceled"
)

// pipeline lists the stages a successful job passes through, in order.
var pipeline = []Status{StatusPending, StatusCloning, StatusZipping, StatusUploading, StatusDone}

var stageLabels = map[Status]string{
	StatusPending: "Queued", StatusCloning: "Downloading", StatusZipping: "Compressing",
	StatusUploading: "Uploading", StatusDone: "Ready", StatusError: "Failed",
	StatusCanceled: "Canceled",
}

func (s Status) Finished() bool { return s == StatusDone || s == StatusError || s == StatusCanceled }

// Progress holds raw live counters. Formatting happens in the API view.
type Progress struct {
	DownloadedBytes int64  `json:"downloaded_bytes,omitempty"`
	Files           int    `json:"files,omitempty"`
	RequestsOK      int    `json:"requests_ok,omitempty"`
	RequestsFailed  int    `json:"requests_failed,omitempty"`
	LastURL         string `json:"last_url,omitempty"`
	BytesPerSecond  int64  `json:"bytes_per_second,omitempty"`
	ZippedFiles     int    `json:"zipped_files,omitempty"`
	ZippedBytes     int64  `json:"zipped_bytes,omitempty"`
	ArchiveBytes    int64  `json:"archive_bytes,omitempty"`
	UploadedBytes   int64  `json:"uploaded_bytes,omitempty"`
}

// Result is recorded once the archive is stored.
type Result struct {
	Filename       string `json:"filename"`
	ArchiveBytes   int64  `json:"archive_bytes"`
	SiteBytes      int64  `json:"site_bytes"`
	Files          int    `json:"files"`
	RequestsOK     int    `json:"requests_ok"`
	RequestsFailed int    `json:"requests_failed"`
	SHA256         string `json:"sha256"`
	QueueMS        int64  `json:"queue_ms"`
	DownloadMS     int64  `json:"download_ms"`
	ZipMS          int64  `json:"zip_ms"`
	UploadMS       int64  `json:"upload_ms"`
	TotalMS        int64  `json:"total_ms"`
}

// Failure explains why a job stopped and at which stage.
type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Stage   Status `json:"stage"`
}

// Job is the stored job state. Field names of the original format are unchanged so
// entries written by older versions still decode.
type Job struct {
	ID             string     `json:"request_id"`
	URL            string     `json:"url"`
	Status         Status     `json:"status"`
	Message        string     `json:"msg,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at,omitzero"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	StageStartedAt *time.Time `json:"stage_started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Progress       Progress   `json:"progress,omitzero"`
	Result         *Result    `json:"result,omitempty"`
	Failure        *Failure   `json:"failure,omitempty"`
	Warnings       []string   `json:"warnings,omitempty"`
	ObjectKey      string     `json:"-"`
}

func (j Job) Host() string {
	if u, err := url.Parse(j.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return j.URL
}

// ---- API representation ----

type stageView struct {
	Key   Status `json:"key"`
	Label string `json:"label"`
	Step  int    `json:"step"`
	Steps int    `json:"steps"`
}

type progressView struct {
	StagePercent   *float64 `json:"stage_percent"`
	DownloadedBy   int64    `json:"downloaded_bytes"`
	Downloaded     string   `json:"downloaded"`
	Files          int      `json:"files"`
	RequestsOK     int      `json:"requests_ok"`
	RequestsFailed int      `json:"requests_failed"`
	LastURL        string   `json:"last_url,omitempty"` // most recently completed request
	BytesPerSecond int64    `json:"bytes_per_second"`
	Rate           string   `json:"rate"`
	LimitBytes     int64    `json:"limit_bytes"`
	Limit          string   `json:"limit"`
	LimitPercent   float64  `json:"limit_percent"`
	ZippedFiles    int      `json:"zipped_files,omitempty"`
	ArchiveBytes   int64    `json:"archive_bytes,omitempty"`
	ArchiveSize    string   `json:"archive_size,omitempty"`
	UploadedBytes  int64    `json:"uploaded_bytes,omitempty"`
	Uploaded       string   `json:"uploaded,omitempty"`
}

type durationsView struct {
	Queue    int64 `json:"queue"`
	Download int64 `json:"download"`
	Zip      int64 `json:"zip"`
	Upload   int64 `json:"upload"`
	Total    int64 `json:"total"`
}

type resultView struct {
	Filename         string        `json:"filename"`
	ArchiveBytes     int64         `json:"archive_bytes"`
	ArchiveSize      string        `json:"archive_size"`
	SiteBytes        int64         `json:"site_bytes"`
	SiteSize         string        `json:"site_size"`
	Files            int           `json:"files"`
	RequestsOK       int           `json:"requests_ok"`
	RequestsFailed   int           `json:"requests_failed"`
	CompressionRatio float64       `json:"compression_ratio"`
	SpaceSaved       float64       `json:"space_saved_percent"`
	SHA256           string        `json:"sha256"`
	DurationsMS      durationsView `json:"durations_ms"`
	Duration         string        `json:"duration"`
	AverageRate      string        `json:"average_download_rate"`
}

type downloadView struct {
	URL              string    `json:"url"`
	Filename         string    `json:"filename"`
	ExpiresAt        time.Time `json:"expires_at"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
}

type JobView struct {
	ID          string            `json:"request_id"`
	URL         string            `json:"url"`
	Host        string            `json:"host"`
	Status      Status            `json:"status"`
	Stage       stageView         `json:"stage"`
	Summary     string            `json:"summary"`
	Message     string            `json:"msg,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	FinishedAt  *time.Time        `json:"finished_at,omitempty"`
	ElapsedMS   int64             `json:"elapsed_ms"`
	Progress    *progressView     `json:"progress,omitempty"`
	Result      *resultView       `json:"result,omitempty"`
	Error       *Failure          `json:"error,omitempty"`
	Warnings    []string          `json:"warnings,omitempty"`
	Download    *downloadView     `json:"download,omitempty"`
	DownloadURL string            `json:"download_url,omitempty"` // Deprecated: use download.url.
	Links       map[string]string `json:"links"`
}

type link struct {
	url     string
	expires time.Time
}

func (j Job) View(limitBytes int64, dl *link, now time.Time) JobView {
	v := JobView{
		ID: j.ID, URL: j.URL, Host: j.Host(), Status: j.Status, Message: j.Message,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt,
		Error: j.Failure, Warnings: j.Warnings, Links: map[string]string{"self": "/api/v1/jobs/" + j.ID},
		Stage: stageView{Key: j.Status, Label: stageLabels[j.Status], Steps: len(pipeline)},
	}
	if v.UpdatedAt.IsZero() {
		v.UpdatedAt = j.CreatedAt
	}
	if !j.Status.Finished() {
		v.Links["cancel"] = "/api/v1/jobs/" + j.ID + "/cancel"
	}
	stageStatus := j.Status
	if j.Failure != nil {
		stageStatus = j.Failure.Stage
	}
	for i, s := range pipeline {
		if s == stageStatus {
			v.Stage.Step = i + 1
		}
	}
	end := now
	if j.FinishedAt != nil {
		end = *j.FinishedAt
	}
	v.ElapsedMS = end.Sub(j.CreatedAt).Milliseconds()

	if j.Status != StatusDone {
		v.Progress = j.progressView(limitBytes)
	}
	if j.Result != nil {
		v.Result = j.Result.view()
	}
	if dl != nil {
		filename := archiveName(j.Host(), j.CreatedAt)
		if j.Result != nil {
			filename = j.Result.Filename
		}
		v.Download = &downloadView{URL: dl.url, Filename: filename, ExpiresAt: dl.expires.UTC(), ExpiresInSeconds: int64(dl.expires.Sub(now).Round(time.Second).Seconds())}
		v.DownloadURL = dl.url
		v.Links["download"] = dl.url
	}
	v.Summary = j.summary(limitBytes)
	return v
}

func (j Job) progressView(limitBytes int64) *progressView {
	p := j.Progress
	v := &progressView{
		DownloadedBy: p.DownloadedBytes, Downloaded: humanize.Bytes(p.DownloadedBytes), Files: p.Files,
		RequestsOK: p.RequestsOK, RequestsFailed: p.RequestsFailed, BytesPerSecond: p.BytesPerSecond, Rate: humanize.Rate(p.BytesPerSecond),
		LimitBytes: limitBytes, Limit: humanize.Bytes(limitBytes), LimitPercent: humanize.Percent(p.DownloadedBytes, limitBytes),
		ZippedFiles: p.ZippedFiles,
	}
	if j.Status == StatusCloning {
		v.LastURL = p.LastURL
	}
	if p.ArchiveBytes > 0 {
		v.ArchiveBytes, v.ArchiveSize = p.ArchiveBytes, humanize.Bytes(p.ArchiveBytes)
	}
	if p.UploadedBytes > 0 {
		v.UploadedBytes, v.Uploaded = p.UploadedBytes, humanize.Bytes(p.UploadedBytes)
	}
	switch j.Status {
	case StatusZipping:
		pct := humanize.Percent(p.ZippedBytes, p.DownloadedBytes)
		v.StagePercent = &pct
	case StatusUploading:
		pct := humanize.Percent(p.UploadedBytes, p.ArchiveBytes)
		v.StagePercent = &pct
	}
	return v
}

func (r *Result) view() *resultView {
	v := &resultView{
		Filename: r.Filename, ArchiveBytes: r.ArchiveBytes, ArchiveSize: humanize.Bytes(r.ArchiveBytes),
		SiteBytes: r.SiteBytes, SiteSize: humanize.Bytes(r.SiteBytes), Files: r.Files,
		RequestsOK: r.RequestsOK, RequestsFailed: r.RequestsFailed, SHA256: r.SHA256,
		DurationsMS: durationsView{Queue: r.QueueMS, Download: r.DownloadMS, Zip: r.ZipMS, Upload: r.UploadMS, Total: r.TotalMS},
		Duration:    logx.Duration(time.Duration(r.TotalMS) * time.Millisecond),
		AverageRate: humanize.Rate(humanize.PerSecond(r.SiteBytes, time.Duration(r.DownloadMS)*time.Millisecond)),
	}
	if r.SiteBytes > 0 {
		v.CompressionRatio = float64(int64(float64(r.ArchiveBytes)/float64(r.SiteBytes)*1000+0.5)) / 1000
		v.SpaceSaved = 100 - humanize.Percent(r.ArchiveBytes, r.SiteBytes)
		if v.SpaceSaved < 0 {
			v.SpaceSaved = 0
		}
	}
	return v
}

// summary is a one-line, human description of where the job is.
func (j Job) summary(limitBytes int64) string {
	p := j.Progress
	switch j.Status {
	case StatusPending:
		return "Waiting for a free worker"
	case StatusCloning:
		if p.Files == 0 {
			return "Connecting to " + j.Host()
		}
		return fmt.Sprintf("Downloading: %s in %d %s at %s (%.0f%% of the %s limit)", humanize.Bytes(p.DownloadedBytes), p.Files, plural(p.Files, "file", "files"), humanize.Rate(p.BytesPerSecond), humanize.Percent(p.DownloadedBytes, limitBytes), humanize.Bytes(limitBytes))
	case StatusZipping:
		return fmt.Sprintf("Compressing: %d of %d files (%.0f%%)", p.ZippedFiles, p.Files, humanize.Percent(p.ZippedBytes, p.DownloadedBytes))
	case StatusUploading:
		return fmt.Sprintf("Uploading: %s of %s (%.0f%%)", humanize.Bytes(p.UploadedBytes), humanize.Bytes(p.ArchiveBytes), humanize.Percent(p.UploadedBytes, p.ArchiveBytes))
	case StatusDone:
		if r := j.Result; r != nil {
			return fmt.Sprintf("Ready: %s ZIP from %s across %d %s, packed in %s", humanize.Bytes(r.ArchiveBytes), humanize.Bytes(r.SiteBytes), r.Files, plural(r.Files, "file", "files"), logx.Duration(time.Duration(r.TotalMS)*time.Millisecond))
		}
		return "Ready to download"
	case StatusCanceled:
		return "Canceled. Temporary files and any attempted upload have been removed."
	case StatusError:
		if j.Failure != nil {
			return j.Failure.Message
		}
		return j.Message
	}
	return string(j.Status)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// archiveName builds a friendly download filename: example.com-2026-10-16.zip
func archiveName(host string, at time.Time) string {
	clean := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, strings.TrimPrefix(host, "www."))
	if clean == "" {
		clean = "website"
	}
	return clean + "-" + at.UTC().Format("2006-01-02") + ".zip"
}
