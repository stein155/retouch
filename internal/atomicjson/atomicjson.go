// Package atomicjson writes a value as indented JSON to a file atomically: it
// writes a temporary file next to the target and renames it into place, so a
// crash or power loss mid-write can't leave a half-written file. Several small
// on-disk stores (presets, settings, marge presets) share this pattern.
package atomicjson

import (
	"encoding/json"
	"io/fs"
	"os"
)

// Write marshals v as indented JSON and atomically replaces path with it, creating
// path with the given file mode.
func Write(path string, v any, mode fs.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
