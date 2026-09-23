package catalog

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadLocal reads the user's own *.gitignore templates from dir. A missing
// directory is not an error.
func LoadLocal(dir string) ([]Template, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Template
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gitignore") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		stem := strings.TrimSuffix(e.Name(), ".gitignore")
		out = append(out, Template{
			Key:     Normalize(stem),
			Name:    stem,
			Group:   "local",
			Source:  SourceLocal,
			Path:    path,
			Content: string(b),
		})
	}
	return out, nil
}
