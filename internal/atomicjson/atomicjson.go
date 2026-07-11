// Package atomicjson writes a value as indented JSON to a file atomically: it
// writes a temporary file next to the target and renames it into place, so a
// crash or power loss mid-write can't leave a half-written file. Several small
// on-disk stores (presets, settings, marge presets) share this pattern.
package atomicjson

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
)

// Write marshals v as indented JSON and atomically replaces path with it, creating
// path with the given file mode.
func Write(path string, v any, mode fs.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	// fsync the data before the rename, else power loss (a speaker's normal
	// off-switch) can journal the rename ahead of the data blocks and leave a
	// zero-length/truncated target — the very corruption this package prevents.
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// fsync the directory so the rename itself survives power loss.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
