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

/* the name is resolved exactly as it arrived. Trimming the surrounding whitespace would make " app.css" and "app.css" name the same file, and the access-control matchers in front of the application compare the raw request path: a rule on "/internal/" does not fire for "/ internal/secret.json", so resolving the trimmed spelling hands out a file the rule was written to protect. The embedded mode never trimmed, so the untrimmed resolution is also the one answer both modes give. */
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

    /* the path that was validated is the one that is opened: os.Open(fullPath) resolves every symlink component a second time, so a component swapped between the check and the open serves a file from outside the base directory. Opening realPath makes the bytes served the ones that were checked. */
    return os.Open(realPath)
}

/* only a retrieval may be answered out of the public directory. Every other method belongs to whatever the application routes the path to, and answering it with the file body hides that route: a DELETE reports success without deleting anything. An OPTIONS preflight is the sharpest case, because a body carries none of the Access-Control-Allow-* headers the browser asked for, so the browser reads the successful answer as a refusal. */
func isRetrievalMethod(method string) bool {
    return nethttp.MethodGet == method || nethttp.MethodHead == method
}

/* a path element that begins with a dot names what a deployment keeps beside its files and never means to publish: .env, .git, .htpasswd. The embedded mode packs them into the binary on purpose, because the embed directive spells "all:public" to keep the packed tree faithful to the directory, so both modes need the refusal. Serving one is worse than a single read: the shipped cache configuration labels the answer publicly cacheable, so a shared cache keeps the copy and hands it to clients that never asked this application for it. The "." and ".." elements never arrive here — a path carrying them is not canonical and is refused before the elements are inspected. */
func hasDotPrefixedPathElement(cleanedPath string, allowedDotPrefixList []string) bool {
    for index, element := range strings.Split(strings.TrimPrefix(cleanedPath, "/"), "/") {
        if false == strings.HasPrefix(element, ".") {
            continue
        }

        /* the allowance reaches the first element only, so a published well-known directory stays retrievable while nothing dot-prefixed below it does: ".well-known/.env" is refused exactly like "/.env" */
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

/* an excluded prefix is the application claiming a part of the url. The file server is the outermost middleware, so what it declines is exactly what reaches the chain the application registered, which is where an authentication check or a policy of its own lives; answering such a request off the disk would step in front of all of it. The comparison is the plain prefix test security.NewPathPrefixMatcher makes against the request path as it arrived — before the strip prefix is removed and before the path is folded — so one spelling written in a firewall rule and here selects the same requests, and an entry that is empty names every path, which is why the configuration refuses one. */
func hasExcludedPathPrefix(requestPath string, excludedPathList []string) bool {
    for _, excludedPath := range excludedPathList {
        if true == strings.HasPrefix(requestPath, excludedPath) {
            return true
        }
    }

    return false
}

func formatContentLength(value int64) string {
    return fmt.Sprintf("%d", value)
}
