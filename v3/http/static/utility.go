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

/* Open resolves a name under the base directory and refuses what would leave it, as confineFileToRoot in the http package does, except that a directory is admitted, a base that cannot be evaluated falls back to its raw spelling, and the refusals are the fs errors io/fs prescribes. The name is not trimmed, since the matchers in front compare the raw path. A change to what "outside the root" means is made in both doors. */
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

    /* the mode is asked before the open, since os.Open on a fifo blocks until a writer appears; a directory passes, anything else that is not a regular file is refused */
    pathInfo, statErr := os.Stat(realPath)
    if nil != statErr {
        return nil, statErr
    }

    if false == pathInfo.Mode().IsRegular() && false == pathInfo.IsDir() {
        return nil, fs.ErrPermission
    }

    /* the validated path is the one opened, which narrows a symlink swap to the window between EvalSymlinks and this open; closing it entirely needs openat2/RESOLVE_BENEATH, which is Linux-only */
    return os.Open(realPath)
}

/* isRetrievalMethod admits only GET and HEAD: any other method belongs to the route the application registered for the path, an OPTIONS preflight included. */
func isRetrievalMethod(method string) bool {
    return nethttp.MethodGet == method || nethttp.MethodHead == method
}

/* hasDotPrefixedPathElement refuses a dot-prefixed element, such as .env or .git, in both modes, since the embed directive "all:public" packs them too. "." and ".." never arrive here: a non-canonical path is refused first. */
func hasDotPrefixedPathElement(cleanedPath string, allowedDotPrefixList []string) bool {
    for index, element := range strings.Split(strings.TrimPrefix(cleanedPath, "/"), "/") {
        if false == strings.HasPrefix(element, ".") {
            continue
        }

        /* the allowance reaches the first element only, so ".well-known/.env" is refused */
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

/* hasExcludedPathPrefix reports a path the application claims, so the request reaches the chain it registered. The test is the plain prefix test security.NewPathPrefixMatcher makes against the path as it arrived, before the strip prefix and any fold, so one spelling selects the same requests in both; an entry with a trailing slash also claims its bare spelling. */
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
