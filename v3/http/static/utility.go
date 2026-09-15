package static

import (
    "fmt"
    "io/fs"
    nethttp "net/http"
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

/* Open resolves names exactly as supplied, without trimming surrounding whitespace, consistently with embedded files and raw-path access control. */
func (instance *dirFileSystem) Open(name string) (fs.File, error) {
    if "" == name {
        return os.Open(instance.basePath)
    }

    cleaned := filepath.Clean(filepath.FromSlash(name))

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

    pathInfo, statErr := os.Stat(realPath)
    if nil != statErr {
        return nil, statErr
    }

    if false == pathInfo.Mode().IsRegular() && false == pathInfo.IsDir() {
        return nil, fs.ErrPermission
    }

    return os.Open(realPath)
}

func isRetrievalMethod(method string) bool {
    return nethttp.MethodGet == method || nethttp.MethodHead == method
}

func hasDotPrefixedPathElement(cleanedPath string, allowedDotPrefixList []string) bool {
    for index, element := range strings.Split(strings.TrimPrefix(cleanedPath, "/"), "/") {
        if false == strings.HasPrefix(element, ".") {
            continue
        }

        if 0 == index {
            allowed := false
            for _, candidate := range allowedDotPrefixList {
                if candidate == element {
                    allowed = true

                    break
                }
            }

            if true == allowed {
                continue
            }
        }

        return true
    }

    return false
}

func hasExcludedPathPrefix(requestPath string, excludedPathList []string) bool {
    for _, excludedPath := range excludedPathList {
        if true == strings.HasPrefix(requestPath, excludedPath) {
            return true
        }

        trimmedExcludedPath := strings.TrimRight(excludedPath, "/")
        if "" != trimmedExcludedPath && requestPath == trimmedExcludedPath {
            return true
        }
    }

    return false
}

func formatContentLength(value int64) string {
    return fmt.Sprintf("%d", value)
}
