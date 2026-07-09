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

    realPath, evalErr := filepath.EvalSymlinks(fullPath)
    if nil != evalErr {
        return nil, evalErr
    }

    realBase, evalBaseErr := filepath.EvalSymlinks(instance.basePath)
    if nil != evalBaseErr {
        realBase = instance.basePath
    }

    if false == strings.HasPrefix(realPath, realBase+string(os.PathSeparator)) && realPath != realBase {
        return nil, fs.ErrPermission
    }

    /* @important open the path that was validated, not the one that was typed: os.Open(fullPath) would resolve every symlink component a second time, and an attacker who swaps a component between the check and the open serves a file from outside the base directory. Opening realPath makes the bytes served the ones that were checked. */
    return os.Open(realPath)
}

func formatContentLength(value int64) string {
    return fmt.Sprintf("%d", value)
}
