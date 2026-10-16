package web

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/TrueFix/pagepack/pkg/config"
	"github.com/TrueFix/pagepack/pkg/humanize"
	"github.com/TrueFix/pagepack/pkg/storage"
	"github.com/TrueFix/pagepack/pkg/version"
	"github.com/gorilla/mux"
	"github.com/valkey-io/valkey-go"
)

type ObjectStore interface {
	Upload(ctx context.Context, key, filename string, o storage.UploadOptions) error
	DownloadURL(ctx context.Context, key string) (string, time.Time, error)
	Delete(ctx context.Context, key string) error
}

type Server struct {
	config   config.Config
	state    StateStore
	objects  ObjectStore
	slots    chan struct{}
	ctx      context.Context
	wg       sync.WaitGroup
	frontend fs.FS
	log      *slog.Logger
	started  time.Time
	activeMu sync.Mutex
	active   map[string]*jobControl
}

func (s *Server) logger() *slog.Logger {
	if s.log == nil {
		return slog.Default()
	}
	return s.log
}

func (s *Server) Router() http.Handler {
	r := mux.NewRouter()
	r.NotFoundHandler = http.HandlerFunc(s.notFound)
	r.MethodNotAllowedHandler = http.HandlerFunc(s.methodNotAllowed)

	// Registered on the root router (not a subrouter) so wrong methods get a JSON 405.
	r.HandleFunc("/api/v1/jobs", s.createJob).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/jobs/{id}", s.getJob).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/jobs/{id}/cancel", s.cancelJob).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/config", s.publicConfig).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/health", s.health).Methods(http.MethodGet)

	// Original endpoints, kept for existing clients. They return the v1 shapes.
	r.HandleFunc("/clone", s.createJob).Methods(http.MethodPost)
	r.HandleFunc("/status", s.legacyStatus).Methods(http.MethodPost)
	r.HandleFunc("/api/config", s.publicConfig).Methods(http.MethodGet)

	r.HandleFunc("/", s.index).Methods(http.MethodGet)
	r.PathPrefix("/assets/").Handler(http.FileServer(http.FS(s.frontend))).Methods(http.MethodGet, http.MethodHead)
	r.Handle("/mark.svg", http.FileServer(http.FS(s.frontend))).Methods(http.MethodGet, http.MethodHead)
	return s.logRequests(r)
}

func StartServer(c config.Config, frontend fs.FS, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	log.Info("pagepack starting", "version", version.Version, "go", runtime.Version(), "pid", os.Getpid())
	wgetPath, err := exec.LookPath(c.WgetPath)
	if err != nil {
		return fmt.Errorf("wget unavailable: %w", err)
	}
	log.Info("wget found", "path", wgetPath, "version", wgetVersion(wgetPath))
	if err := os.MkdirAll(c.TempDir, 0700); err != nil {
		return err
	}
	if _, err := fs.Stat(frontend, "index.html"); err != nil {
		return fmt.Errorf("frontend build missing; run npm run build: %w", err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	opt, err := valkey.ParseURL(c.ValkeyURL)
	if err != nil {
		return fmt.Errorf("VALKEY_URL: %w", err)
	}
	client, err := valkey.NewClient(opt)
	if err != nil {
		return fmt.Errorf("connect Valkey: %w", err)
	}
	defer client.Close()
	state := &ValkeyState{client: client, ttl: c.StateTTL}
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	pingStart := time.Now()
	err = state.Ping(pingCtx)
	pingCancel()
	if err != nil {
		return fmt.Errorf("Valkey ping: %w", err)
	}
	log.Info("valkey connected", "addr", redactedHost(c.ValkeyURL), "took", time.Since(pingStart))

	objects, err := storage.New(ctx, c)
	if err != nil {
		return err
	}
	endpoint := c.S3Endpoint
	if endpoint == "" {
		endpoint = "aws"
	}
	log.Info("storage ready", "bucket", c.S3Bucket, "region", c.S3Region, "endpoint", redactedHost(endpoint), "prefix", c.S3Prefix)
	log.Info("limits", "workers", c.MaxConcurrentJobs, "max_site", humanize.Bytes(c.MaxWebsiteBytes), "job_timeout", c.JobTimeout,
		"link_timeout", c.WgetTimeout, "link_attempts", c.WgetTries,
		"state_ttl", c.StateTTL, "download_ttl", c.DownloadTTL, "temp", c.TempDir, "log_level", c.LogLevel.String())

	s := &Server{config: c, state: state, objects: objects, slots: make(chan struct{}, c.MaxConcurrentJobs), ctx: ctx, frontend: frontend, log: log, started: time.Now()}
	httpServer := &http.Server{Addr: c.HTTPAddr, Handler: s.Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		<-ctx.Done()
		log.Info("shutting down", "active_jobs", len(s.slots))
		shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Warn("HTTP shutdown", "err", err)
		}
	}()
	log.Info("listening", "url", "http://"+displayAddr(c.HTTPAddr), "api", "http://"+displayAddr(c.HTTPAddr)+"/api/v1")
	err = httpServer.ListenAndServe()
	cancel()
	<-finished
	if n := len(s.slots); n > 0 {
		log.Info("waiting for jobs to stop", "active_jobs", n)
	}
	s.wg.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		log.Info("stopped", "uptime", time.Since(s.started).Round(time.Second))
		return nil
	}
	return err
}

func wgetVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "unknown"
	}
	line, _, _ := strings.Cut(string(out), "\n")
	line, _, _ = strings.Cut(line, " built on")
	return strings.TrimSpace(line)
}

// redactedHost shows where a URL points without credentials or query parameters.
func redactedHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	return u.Scheme + "://" + u.Host + strings.TrimSuffix(u.Path, "/")
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}
