package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigValidation(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("AWS_BUCKET", "")
	t.Setenv("AWS_ENDPOINT", "")
	t.Setenv("S3_BUCKET", "test-bucket")
	for _, tc := range []struct{ name, value string }{
		{"MAX_WEBSITE_BYTES", "0"}, {"MAX_WEBSITE_BYTES", "garbage"},
		{"MAX_CONCURRENT_JOBS", "-1"}, {"JOB_TIMEOUT", "invalid"},
		{"DOWNLOAD_TTL", "169h"}, {"STATE_TTL", "1s"},
		{"S3_FORCE_PATH_STYLE", "invalid"}, {"S3_ENDPOINT", "not-a-url"},
		{"LOG_LEVEL", "loud"}, {"LOG_FORMAT", "xml"},
		{"WGET_TIMEOUT", "0s"}, {"WGET_TIMEOUT", "invalid"},
		{"WGET_TRIES", "0"}, {"WGET_TRIES", "11"}, {"WGET_TRIES", "invalid"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	t.Setenv("MAX_WEBSITE_BYTES", "2048")
	t.Setenv("TEMP_DIR", "./tmp/pagepack")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxWebsiteBytes != 2048 || c.LogLevel != slog.LevelInfo || c.LogFormat != "pretty" {
		t.Fatalf("unexpected config: size=%d level=%v format=%q", c.MaxWebsiteBytes, c.LogLevel, c.LogFormat)
	}
	if c.WgetTimeout != 10*time.Second || c.WgetTries != 1 {
		t.Fatalf("unexpected wget defaults: timeout=%s tries=%d", c.WgetTimeout, c.WgetTries)
	}
	if c.TempDir != "./tmp/pagepack" {
		t.Fatalf("TEMP_DIR override = %q", c.TempDir)
	}
}

func TestValkeySchemesAndAWSAliases(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("AWS_BUCKET", "pagepack-test")
	t.Setenv("AWS_ENDPOINT", "https://blr1.digitaloceanspaces.com")
	t.Setenv("AWS_REGION", "blr1")
	t.Setenv("AWS_BUCKET_PATH", "1site/pagepack")
	t.Setenv("S3_BUCKET", "fallback-bucket")
	t.Setenv("S3_ENDPOINT", "https://fallback.example")
	t.Setenv("S3_PREFIX", "fallback/")
	for _, tc := range []struct{ input, want string }{
		{"valkey://default:test@localhost:6379/5", "redis://default:test@localhost:6379/5"},
		{"valkeys://default:test@localhost:6379/5", "rediss://default:test@localhost:6379/5"},
		{"redis://localhost:6379/5", "redis://localhost:6379/5"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Setenv("VALKEY_URL", tc.input)
			c, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if c.ValkeyURL != tc.want {
				t.Errorf("URL normalization = %q", c.ValkeyURL)
			}
			if c.S3Bucket != "pagepack-test" || c.S3Endpoint != "https://blr1.digitaloceanspaces.com" || c.S3Region != "blr1" || c.S3Prefix != "1site/pagepack" {
				t.Fatal("AWS storage settings were not applied")
			}
		})
	}
}

func TestDotEnvLoadsAndPreservesExportedValues(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// Register cleanup before clearing variables so dotenv can populate them.
	for _, name := range []string{"AWS_BUCKET", "MAX_WEBSITE_BYTES"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HTTP_ADDR", "localhost:9000")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("AWS_BUCKET=dotenv-bucket\nMAX_WEBSITE_BYTES=2048\nHTTP_ADDR=localhost:8000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.S3Bucket != "dotenv-bucket" || c.MaxWebsiteBytes != 2048 || c.HTTPAddr != "localhost:9000" {
		t.Fatal("dotenv values or environment precedence are incorrect")
	}
}
