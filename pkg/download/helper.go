package download

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Workspace is a private per-job scratch directory: Site holds the mirror, Archive the ZIP.
type Workspace struct {
	Dir     string
	Site    string
	Archive string
}

func NewWorkspace(tempDir string) (*Workspace, error) {
	dir, err := os.MkdirTemp(tempDir, "pagepack-*")
	if err != nil {
		return nil, err
	}
	w := &Workspace{Dir: dir, Site: filepath.Join(dir, "site"), Archive: filepath.Join(dir, "website.zip")}
	if err := os.Mkdir(w.Site, 0700); err != nil {
		w.Cleanup()
		return nil, err
	}
	return w, nil
}

func (w *Workspace) Cleanup() error { return os.RemoveAll(w.Dir) }

// DirStats returns the total size and number of regular files beneath path.
// Files that disappear mid-walk (wget renames temporaries) are skipped, not fatal.
func DirStats(path string) (bytes int64, files int, err error) {
	err = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
			files++
		}
		return nil
	})
	return bytes, files, err
}
