package static

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type dirFileSystem struct {
	basePath string
}

func osDirFileSystem(basePath string) fs.FS {
	return &dirFileSystem{
		basePath: basePath,
	}
}

func (instance *dirFileSystem) Open(name string) (fs.File, error) {
	trimmedName := strings.TrimSpace(name)
	if "" == trimmedName {
		return os.Open(instance.basePath)
	}

	cleaned := filepath.Clean(filepath.FromSlash(trimmedName))

	if true == filepath.IsAbs(cleaned) {
		return nil, fs.ErrInvalid
	}

	if "." == cleaned {
		cleaned = ""
	}

	if ".." == cleaned || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return nil, fs.ErrPermission
	}

	fullPath := instance.basePath
	if "" != cleaned {
		fullPath = instance.basePath + string(os.PathSeparator) + cleaned
	}

	return os.Open(fullPath)
}

func formatContentLength(value int64) string {
	return fmt.Sprintf("%d", value)
}
