package download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/TrueFix/pagepack/pkg/humanize"
)

// MirrorStats is a live snapshot of a running mirror.
type MirrorStats struct {
	Bytes          int64         // bytes written to disk so far
	Files          int           // regular files on disk so far
	Fetched        int           // successful requests reported by wget
	FailedRequests int           // failed HTTP/network requests
	LastURL        string        // most recently fetched URL
	BytesPerSecond int64         // smoothed download rate
	Elapsed        time.Duration // time since wget started
}

type MirrorResult struct {
	MirrorStats
	Warnings []string
}

type MirrorOptions struct {
	URL      string
	WgetPath string
	MaxBytes int64
	// Timeout applies to DNS, connecting, and read inactivity, not total file time.
	Timeout time.Duration
	Tries   int
	// Interval between size checks and progress callbacks; defaults to 250ms.
	Interval   time.Duration
	OnProgress func(MirrorStats)
	Log        *slog.Logger
}

// Mirror downloads a website with wget into ws.Site, enforcing MaxBytes while it runs.
func Mirror(ctx context.Context, ws *Workspace, o MirrorOptions) (MirrorResult, error) {
	if o.Interval <= 0 {
		o.Interval = 250 * time.Millisecond
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.Tries <= 0 {
		o.Tries = 1
	}
	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, o.WgetPath, "--mirror", "--convert-links", "--adjust-extension", "--page-requisites",
		"--no-parent", "--verbose", "--progress=dot:giga", fmt.Sprintf("--timeout=%.3f", o.Timeout.Seconds()), fmt.Sprintf("--tries=%d", o.Tries),
		"--max-redirect=10", "--directory-prefix", ws.Site, "--", o.URL)
	// Parsing uses GNU wget's English diagnostics regardless of the server locale.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return MirrorResult{}, Fail(CodeInternal, "Couldn't start the downloader", err)
	}
	if err := cmd.Start(); err != nil {
		return MirrorResult{}, Fail(CodeInternal, "Couldn't start the downloader (is wget installed?)", err)
	}
	started := time.Now()
	parser := &wgetLog{log: o.Log}
	parsed := make(chan struct{})
	go func() { defer close(parsed); parser.consume(stderr) }()
	done := make(chan error, 1)
	go func() { <-parsed; done <- cmd.Wait() }()

	var stats MirrorStats
	var lastBytes int64
	lastSample := started
	sample := func() error {
		bytes, files, err := DirStats(ws.Site)
		if err != nil {
			return Fail(CodeInternal, "Couldn't measure the download", err)
		}
		now := time.Now()
		if dt := now.Sub(lastSample); dt > 0 {
			instant := humanize.PerSecond(bytes-lastBytes, dt)
			if stats.BytesPerSecond == 0 {
				stats.BytesPerSecond = instant
			} else {
				stats.BytesPerSecond = (stats.BytesPerSecond*7 + instant*3) / 10
			}
		}
		lastBytes, lastSample = bytes, now
		stats.Bytes, stats.Files, stats.Elapsed = bytes, files, now.Sub(started)
		stats.Fetched, stats.FailedRequests, stats.LastURL = parser.snapshot()
		if bytes > o.MaxBytes {
			return Fail(CodeTooLarge, fmt.Sprintf("Website is larger than the %s limit (stopped at %s)", humanize.Bytes(o.MaxBytes), humanize.Bytes(bytes)), nil)
		}
		return nil
	}
	stop := func() { cancel(); <-done }

	ticker := time.NewTicker(o.Interval)
	defer ticker.Stop()
	for {
		select {
		case waitErr := <-done:
			if ctx.Err() != nil {
				return MirrorResult{MirrorStats: stats}, contextError(ctx)
			}
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 4 {
				if err := parser.removeIncomplete(ws.Site); err != nil {
					return MirrorResult{MirrorStats: stats}, Fail(CodeInternal, "Couldn't remove incomplete downloads", err)
				}
			}
			if err := sample(); err != nil {
				return MirrorResult{MirrorStats: stats}, err
			}
			// Report an overall average at the end rather than the last instantaneous rate.
			stats.BytesPerSecond = humanize.PerSecond(stats.Bytes, stats.Elapsed)
			warnings, failure := interpretExit(waitErr, stats, parser)
			if failure != nil { // never return a typed nil *Error as a non-nil error
				return MirrorResult{MirrorStats: stats}, failure
			}
			return MirrorResult{MirrorStats: stats, Warnings: warnings}, nil
		case <-ticker.C:
			if err := sample(); err != nil {
				stop()
				return MirrorResult{MirrorStats: stats}, err
			}
			if o.OnProgress != nil {
				o.OnProgress(stats)
			}
		case <-ctx.Done():
			stop()
			return MirrorResult{MirrorStats: stats}, contextError(ctx)
		}
	}
}

// interpretExit maps wget's exit status to warnings or a classified failure.
// See "Exit Status" in the GNU wget manual.
func interpretExit(waitErr error, stats MirrorStats, log *wgetLog) ([]string, *Error) {
	code := 0
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		code = exitErr.ExitCode()
	} else if waitErr != nil {
		return nil, Fail(CodeWget, "The downloader stopped unexpectedly", waitErr)
	}
	last := log.lastError()
	var warnings []string
	if stats.FailedRequests > 0 {
		warnings = append(warnings, fmt.Sprintf("%d %s failed and %s skipped", stats.FailedRequests, plural(stats.FailedRequests, "request", "requests"), plural(stats.FailedRequests, "was", "were")))
	}
	switch code {
	case 0:
	case 8:
		// A server error for some links (often a missing image) still leaves a usable mirror.
		if stats.Files == 0 || stats.Fetched == 0 {
			return nil, Fail(CodeServer, withDetail("The website responded with an error", last), nil)
		}
	case 4:
		if stats.Files == 0 || stats.Fetched == 0 {
			return nil, Fail(CodeNetwork, withDetail("Couldn't reach the website", last), nil)
		}
		warnings = append(warnings, withDetail("Some links timed out or had network errors; only completed downloads were archived", last))
	case 5:
		return nil, Fail(CodeTLS, withDetail("The website's TLS certificate couldn't be verified", last), nil)
	case 6:
		return nil, Fail(CodeAuth, "The website requires a username and password", nil)
	default:
		return nil, Fail(CodeWget, withDetail(fmt.Sprintf("wget exited with status %d", code), last), waitErr)
	}
	if stats.Files == 0 {
		return nil, Fail(CodeEmpty, withDetail("Nothing could be downloaded from this address", last), nil)
	}
	return warnings, nil
}

func withDetail(message, detail string) string {
	if detail == "" {
		return message
	}
	return message + " (" + detail + ")"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

var (
	reFetched  = regexp.MustCompile(`URL:\s*(\S+)\s+\[`)
	reHTTPErr  = regexp.MustCompile(`ERROR (\d{3}): (.+?)\.?$`)
	reSaved    = regexp.MustCompile(`-> "(.+)" \[`)
	reStarted  = regexp.MustCompile(`^--[^ ]+ [^ ]+--\s+(https?://\S+)`)
	reFinished = regexp.MustCompile(`['"](.+)['"] saved \[`)
)

// wgetLog tracks the URL and completed path for each transfer. Verbose diagnostics
// are necessary: --no-verbose hides some interrupted-body errors entirely.
type wgetLog struct {
	mu         sync.Mutex
	fetched    int
	failed     int
	lastURL    string
	pendingURL string
	errors     []string
	log        *slog.Logger
	completed  map[string]bool
}

func (w *wgetLog) consume(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		w.line(strings.TrimSpace(scanner.Text()))
	}
}

func (w *wgetLog) line(line string) {
	if line == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if start := reStarted.FindStringSubmatch(line); start != nil {
		w.pendingURL = start[1]
		return
	}
	if saved := reFinished.FindStringSubmatch(line); saved != nil {
		w.fetched++
		w.lastURL = w.pendingURL
		if w.completed == nil {
			w.completed = make(map[string]bool)
		}
		w.completed[filepath.Clean(saved[1])] = true
		w.log.Debug("fetched", "url", w.lastURL)
		return
	}
	switch m := reFetched.FindStringSubmatch(line); {
	case m != nil:
		w.fetched++
		w.lastURL = m[1]
		if saved := reSaved.FindStringSubmatch(line); saved != nil {
			if w.completed == nil {
				w.completed = make(map[string]bool)
			}
			w.completed[filepath.Clean(saved[1])] = true
		}
		w.log.Debug("fetched", "url", m[1])
	case strings.HasSuffix(line, ":") && (strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://")):
		w.pendingURL = strings.TrimSuffix(line, ":")
	default:
		if e := reHTTPErr.FindStringSubmatch(line); e != nil {
			// wget asks for robots.txt on its own; most sites don't have one.
			if strings.HasSuffix(w.pendingURL, "/robots.txt") {
				w.pendingURL = ""
				return
			}
			w.failed++
			w.remember(fmt.Sprintf("%s %s %s", e[1], e[2], w.pendingURL))
			w.log.Debug("request failed", "http_status", e[1], "reason", e[2], "url", w.pendingURL)
			w.pendingURL = ""
			return
		}
		w.log.Debug("wget diagnostic", "line", line)
		lower := strings.ToLower(line)
		// Match diagnostics, not arbitrary words such as "ssl" in a local path.
		if strings.HasPrefix(lower, "saving to:") || strings.HasPrefix(lower, "converting links in ") {
			return
		}
		for _, hint := range []string{"read error", "unable to resolve", "connection refused", "timed out", "no route to host", "unable to establish ssl", "error: cannot verify", "authentication failed", "failed:"} {
			if strings.Contains(lower, hint) {
				w.remember(strings.TrimPrefix(line, "wget: "))
				w.failed++
				w.log.Warn("link failed; skipping", "url", w.pendingURL, "reason", line)
				return
			}
		}
	}
}

// A failed transfer can leave a partial file on disk. After a network-error exit,
// retain only paths wget explicitly reported as completed. Never walk outside Site.
func (w *wgetLog) removeIncomplete(site string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return filepath.Walk(site, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && !w.completed[filepath.Clean(path)] {
			w.log.Debug("removing incomplete download", "path", path)
			return os.Remove(path)
		}
		return nil
	})
}

func (w *wgetLog) remember(message string) {
	w.errors = append(w.errors, strings.TrimSpace(message))
	if len(w.errors) > 5 {
		w.errors = w.errors[1:]
	}
}

func (w *wgetLog) snapshot() (fetched, failed int, lastURL string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fetched, w.failed, w.lastURL
}

func (w *wgetLog) lastError() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.errors) == 0 {
		return ""
	}
	return w.errors[len(w.errors)-1]
}
