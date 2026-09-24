package runner

import (
	"go/version"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// moduleGoVersion reads the nearest go.mod; an unreadable or invalid module is unknown.
func moduleGoVersion(dir string) string {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(path)
		if err == nil {
			file, err := modfile.Parse(path, data, nil)
			if err != nil || file.Go == nil {
				return ""
			}
			v := "go" + file.Go.Version
			if version.IsValid(v) {
				return v
			}
			return ""
		}
		if !os.IsNotExist(err) {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
