package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/TrueFix/pagepack/pkg/logx"
	"github.com/joho/godotenv"
)

// Config contains application environment settings. Sizes are bytes; durations use Go syntax.
type Config struct {
	HTTPAddr, TempDir, WgetPath                         string
	MaxWebsiteBytes                                     int64
	MaxConcurrentJobs                                   int
	JobTimeout, StateTTL, DownloadTTL                   time.Duration
	ValkeyURL                                           string
	S3Bucket, S3Region, S3Endpoint, S3Prefix            string
	S3ForcePathStyle                                    bool
	AWSAccessKeyID, AWSSecretAccessKey, AWSSessionToken string
	LogLevel                                            slog.Level
	LogFormat                                           string
	WgetTimeout                                         time.Duration
	WgetTries                                           int
}

func Load() (Config, error) {
	// Existing process environment wins over .env values.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	c := Config{
		HTTPAddr: env("HTTP_ADDR", "localhost:8000"), TempDir: env("TEMP_DIR", os.TempDir()),
		WgetPath: env("WGET_PATH", "wget"), ValkeyURL: env("VALKEY_URL", "valkey://localhost:6379"),
		S3Bucket: env("AWS_BUCKET", os.Getenv("S3_BUCKET")), S3Region: env("AWS_REGION", "us-east-1"),
		S3Endpoint: env("AWS_ENDPOINT", os.Getenv("S3_ENDPOINT")), S3Prefix: env("AWS_BUCKET_PATH", env("S3_PREFIX", "clones/")),
		AWSAccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), AWSSecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"), AWSSessionToken: os.Getenv("AWS_SESSION_TOKEN"),
	}
	var err error
	if c.LogLevel, err = logx.ParseLevel(env("LOG_LEVEL", "info")); err != nil {
		return c, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	switch c.LogFormat = env("LOG_FORMAT", logx.FormatPretty); c.LogFormat {
	case logx.FormatPretty, logx.FormatJSON, logx.FormatText:
	default:
		return c, fmt.Errorf("LOG_FORMAT must be pretty, json, or text")
	}
	// Valkey uses the Redis wire protocol; normalize its URL scheme for the client.
	valkeyURL, err := url.Parse(c.ValkeyURL)
	if err != nil || valkeyURL.Hostname() == "" {
		return c, fmt.Errorf("VALKEY_URL must be a valid Valkey connection URL")
	}
	switch valkeyURL.Scheme {
	case "valkey":
		valkeyURL.Scheme = "redis"
	case "valkeys":
		valkeyURL.Scheme = "rediss"
	case "redis", "rediss":
	default:
		return c, fmt.Errorf("VALKEY_URL must use valkey://, valkeys://, redis://, or rediss://")
	}
	c.ValkeyURL = valkeyURL.String()
	if c.MaxWebsiteBytes, err = strconv.ParseInt(env("MAX_WEBSITE_BYTES", "104857600"), 10, 64); err != nil || c.MaxWebsiteBytes <= 0 {
		return c, fmt.Errorf("MAX_WEBSITE_BYTES must be a positive integer")
	}
	if c.MaxConcurrentJobs, err = strconv.Atoi(env("MAX_CONCURRENT_JOBS", "4")); err != nil || c.MaxConcurrentJobs <= 0 {
		return c, fmt.Errorf("MAX_CONCURRENT_JOBS must be a positive integer")
	}
	if c.WgetTries, err = strconv.Atoi(env("WGET_TRIES", "1")); err != nil || c.WgetTries < 1 || c.WgetTries > 10 {
		return c, fmt.Errorf("WGET_TRIES must be between 1 and 10")
	}
	for _, setting := range []struct {
		name, fallback string
		target         *time.Duration
	}{
		{"JOB_TIMEOUT", "15m", &c.JobTimeout}, {"STATE_TTL", "24h", &c.StateTTL}, {"DOWNLOAD_TTL", "15m", &c.DownloadTTL},
		{"WGET_TIMEOUT", "10s", &c.WgetTimeout},
	} {
		*setting.target, err = time.ParseDuration(env(setting.name, setting.fallback))
		if err != nil || *setting.target < time.Second {
			return c, fmt.Errorf("%s must be at least 1s", setting.name)
		}
	}
	if c.StateTTL <= c.JobTimeout {
		return c, fmt.Errorf("STATE_TTL must exceed JOB_TIMEOUT")
	}
	if c.DownloadTTL > 7*24*time.Hour {
		return c, fmt.Errorf("DOWNLOAD_TTL cannot exceed 7 days")
	}
	if c.S3ForcePathStyle, err = strconv.ParseBool(env("S3_FORCE_PATH_STYLE", "false")); err != nil {
		return c, fmt.Errorf("invalid S3_FORCE_PATH_STYLE: %w", err)
	}
	if c.S3Bucket == "" {
		return c, fmt.Errorf("AWS_BUCKET (or S3_BUCKET) is required")
	}
	if (c.AWSAccessKeyID == "") != (c.AWSSecretAccessKey == "") {
		return c, fmt.Errorf("AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY must be set together")
	}
	if c.S3Endpoint != "" {
		u, e := url.Parse(c.S3Endpoint)
		if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("AWS_ENDPOINT (or S3_ENDPOINT) must be an HTTP(S) URL")
		}
	}
	return c, nil
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
