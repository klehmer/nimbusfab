package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileBackend reads JSON secret files from a directory. Convention:
// ref "aws-dev" resolves to "<Dir>/aws-dev.json". Missing files return
// (nil, nil) so the backend can be chained.
type FileBackend struct {
	Dir string
}

// NewFileBackend returns a FileBackend. If dir is empty, defaults to
// ~/.nimbusfab/secrets when the user's home directory is resolvable;
// otherwise leaves Dir empty (Resolve will return nil/nil).
func NewFileBackend(dir string) *FileBackend {
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".nimbusfab", "secrets")
		}
	}
	return &FileBackend{Dir: dir}
}

// Kind returns "file".
func (*FileBackend) Kind() string { return "file" }

// Resolve reads "<Dir>/<ref>.json" and returns the parsed object.
func (b *FileBackend) Resolve(ctx context.Context, ref string) (map[string]any, error) {
	_ = ctx
	if ref == "" || b.Dir == "" {
		return nil, nil
	}
	path := filepath.Join(b.Dir, ref+".json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secrets/file: read %s: %w", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("secrets/file: %s invalid JSON: %w", path, err)
	}
	return out, nil
}
