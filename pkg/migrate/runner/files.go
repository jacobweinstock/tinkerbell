package runner

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// objectFilename returns the per-object filename used by the workdir
// layout: <namespace>__<name>.yaml. Cluster-scoped objects use an
// empty namespace and produce __<name>.yaml.
func objectFilename(namespace, name string) string {
	return namespace + "__" + name + ".yaml"
}

// parseObjectFilename is the inverse of objectFilename.
func parseObjectFilename(base string) (namespace, name string, ok bool) {
	if !strings.HasSuffix(base, ".yaml") {
		return "", "", false
	}
	trimmed := strings.TrimSuffix(base, ".yaml")
	idx := strings.Index(trimmed, "__")
	if idx < 0 {
		return "", "", false
	}
	return trimmed[:idx], trimmed[idx+2:], true
}

// writeFileAtomic writes data to path using a temp-and-rename pattern.
// The parent directory must already exist.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// listYAMLs returns the names (basenames) of all .yaml files in dir.
// A missing dir is treated as empty.
func listYAMLs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
