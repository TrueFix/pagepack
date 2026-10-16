package download

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ZipStats is a live snapshot while compressing.
type ZipStats struct {
	Files      int   // files written so far
	TotalFiles int   // files to write
	Bytes      int64 // source bytes read so far
	TotalBytes int64 // source bytes to read
}

type ZipResult struct {
	Path    string
	Bytes   int64 // size of the finished archive
	Files   int
	SHA256  string
	Elapsed time.Duration
}

type ZipOptions struct {
	TotalFiles int
	TotalBytes int64
	// Interval throttles OnProgress; defaults to 200ms. A final update is always sent.
	Interval   time.Duration
	OnProgress func(ZipStats)
}

// Zip compresses source into target, hashing the archive as it is written.
// The target is removed if anything fails.
func Zip(ctx context.Context, source, target string, o ZipOptions) (result ZipResult, err error) {
	if o.Interval <= 0 {
		o.Interval = 200 * time.Millisecond
	}
	started := time.Now()
	f, err := os.Create(target)
	if err != nil {
		return result, Fail(CodeArchive, "Couldn't create the ZIP file", err)
	}
	hash := sha256.New()
	counter := &countingWriter{}
	z := zip.NewWriter(io.MultiWriter(f, hash, counter))
	defer func() {
		if e := z.Close(); err == nil && e != nil {
			err = Fail(CodeArchive, "Couldn't finish the ZIP file", e)
		}
		if e := f.Close(); err == nil && e != nil {
			err = Fail(CodeArchive, "Couldn't save the ZIP file", e)
		}
		if err != nil {
			_ = os.Remove(target)
			return
		}
		result = ZipResult{Path: target, Bytes: counter.n, Files: result.Files, SHA256: hex.EncodeToString(hash.Sum(nil)), Elapsed: time.Since(started)}
	}()

	stats := ZipStats{TotalFiles: o.TotalFiles, TotalBytes: o.TotalBytes}
	lastReport := time.Time{}
	report := func(force bool) {
		if o.OnProgress != nil && (force || time.Since(lastReport) >= o.Interval) {
			lastReport = time.Now()
			o.OnProgress(stats)
		}
	}
	walkErr := filepath.Walk(source, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == source {
			return nil
		}
		name, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		if info.IsDir() {
			header.Name += "/"
		} else {
			header.Method = zip.Deflate
		}
		writer, err := z.CreateHeader(header)
		if err != nil || info.IsDir() {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(writer, contextReader{ctx: ctx, reader: file, onRead: func(n int) {
			stats.Bytes += int64(n)
			report(false)
		}})
		if err != nil {
			return err
		}
		stats.Files++
		report(false)
		return nil
	})
	if walkErr != nil {
		if ctx.Err() != nil {
			return result, contextError(ctx)
		}
		return result, Fail(CodeArchive, "Couldn't add a file to the ZIP", walkErr)
	}
	report(true)
	result.Files = stats.Files
	return result, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
	onRead func(int)
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if n > 0 && r.onRead != nil {
		r.onRead(n)
	}
	return n, err
}

type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }
