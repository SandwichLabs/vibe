package awssso

import (
	"fmt"
	"path/filepath"
)

func sprintf(f string, a ...any) string { return fmt.Sprintf(f, a...) }

func filepathDir(p string) string { return filepath.Dir(p) }
