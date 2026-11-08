package static

import (
    "fmt"
    "io"
    "io/fs"
    "mime"
    nethttp "net/http"
    "path"
    "path/filepath"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    "github.com/precision-soft/melody/v2/logging"
    loggingcontract "github.com/precision-soft/melody/v2/logging/contract"
)

type FileServer struct {
    config      *FileServerConfig
    fileSystem  fs.FS
    contentTags *sync.Map
}

func NewFileServer(options *Options) *FileServer {
    /* nil options are refused by name: the default would be a live file server over the "public" directory */
    if nil == options {
        exception.Panic(
            exception.NewError("options are required for the static file server", nil, nil),
        )
    }

    fileSystem := options.fileSystem

    if ModeFilesystem == options.fileServerConfig.mode {
        publicDir := strings.TrimSpace(options.fileServerConfig.publicDir)
        if "" == publicDir {
            publicDir = "public"
        }

        if true == filepath.IsAbs(publicDir) {
            fileSystem = osDirFileSystem(publicDir)
        } else {
            root := strings.TrimSpace(options.root)
            if "" == root {
                root = "."
            }

            absolutePublicDir := filepath.Join(root, publicDir)

            fileSystem = osDirFileSystem(absolutePublicDir)
        }
    }

    if nil == fileSystem {
        exception.Panic(exception.NewError("file system may not be nil for the file server", nil, nil))
    }

    /* a public directory the build did not embed is a wiring fault: ignoring the key dissolves the stripped prefix confinement */
    if ModeEmbedded == options.fileServerConfig.mode {
        embeddedPublicDir := strings.TrimSpace(options.fileServerConfig.publicDir)
        if "" != embeddedPublicDir {
            publicDirInfo, publicDirErr := fs.Stat(fileSystem, embeddedPublicDir)
            if nil != publicDirErr || false == publicDirInfo.IsDir() {
                exception.Panic(
                    exception.NewError(
                        "the public directory is not present in the embedded file system",
                        exceptioncontract.Context{
                            "publicDir": embeddedPublicDir,
                        },
                        publicDirErr,
                    ),
                )
            }
        }
    }

    /* copied, struct and both lists: in-flight requests read these fields with no lock, so the server is immutable once built */
    configCopy := *options.fileServerConfig
    configCopy.allowedDotPrefixList = append([]string{}, options.fileServerConfig.allowedDotPrefixList...)
    configCopy.excludedPathList = append([]string{}, options.fileServerConfig.excludedPathList...)
    config := &configCopy

    if "" == config.indexFile {
        config.indexFile = "index.html"
    }

    /* an explicit zero is honoured as max-age=0; only a negative value reads as unset and takes the default */
    if true == config.enableCache && 0 > config.cacheMaxAge {
        config.cacheMaxAge = 3600
    }

    return &FileServer{
        config:      config,
        fileSystem:  fileSystem,
        contentTags: &sync.Map{},
    }
}

func (instance *FileServer) ServeReader(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (int, nethttp.Header, io.ReadCloser, bool) {
    logger = logging.EnsureLogger(logger)

    if true == internal.IsNilInterface(request) {
        logger.Warning("static serve reader skipped because request is nil", nil)

        return 0, nil, nil, false
    }

    statusCode, headers, file, fileInfo, ok := instance.serveForStreaming(request, logger)
    if false == ok {
        return 0, nil, nil, false
    }

    if nethttp.StatusNotModified == statusCode {
        return statusCode, headers, nil, true
    }

    if nethttp.MethodHead == request.HttpRequest().Method {
        if nil == headers {
            headers = nethttp.Header{}
        }

        headers.Set("Content-Length", formatContentLength(fileInfo.Size()))

        _ = file.Close()
        return nethttp.StatusOK, headers, nil, true
    }

    readCloser, ok := file.(io.ReadCloser)
    if false == ok {
        _ = file.Close()

        logger.Error(
            "static serve reader file is not a read closer",
            loggingcontract.Context{
                "type": fmt.Sprintf("%T", file),
            },
        )

        return 0, nil, nil, false
    }

    if nil == headers {
        headers = nethttp.Header{}
    }

    if 0 < fileInfo.Size() {
        headers.Set("Content-Length", formatContentLength(fileInfo.Size()))
    }

    return nethttp.StatusOK, headers, readCloser, true
}

func (instance *FileServer) Serve(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (int, nethttp.Header, []byte, bool) {
    logger = logging.EnsureLogger(logger)

    if true == internal.IsNilInterface(request) {
        logger.Warning("static serve skipped because request is nil", nil)

        return 0, nil, nil, false
    }

    method := request.HttpRequest().Method

    if false == isRetrievalMethod(method) {
        /* debug, not info: with the middleware registered globally this fires for every POST and PUT */
        logger.Debug(
            "static serve method not eligible",
            loggingcontract.Context{
                "method": method,
            },
        )

        return 0, nil, nil, false
    }

    requestPath := request.HttpRequest().URL.Path

    if true == hasExcludedPathPrefix(requestPath, instance.config.excludedPathList) {
        logger.Debug(
            "static serve excluded path",
            loggingcontract.Context{
                "path": requestPath,
            },
        )

        return 0, nil, nil, false
    }

    if "" != instance.config.stripPrefix {
        if strings.HasPrefix(requestPath, instance.config.stripPrefix) {
            logger.Debug(
                "static serve strip prefix match",
                loggingcontract.Context{
                    "path":        requestPath,
                    "stripPrefix": instance.config.stripPrefix,
                },
            )

            requestPath = strings.TrimPrefix(requestPath, instance.config.stripPrefix)
            if "" == requestPath {
                requestPath = "/"
            }
        } else {
            /* debug, not info: every request outside the mounted prefix takes this exit */
            logger.Debug(
                "static serve strip prefix mismatch",
                loggingcontract.Context{
                    "path":        requestPath,
                    "stripPrefix": instance.config.stripPrefix,
                },
            )

            return 0, nil, nil, false
        }
    } else {
        logger.Debug(
            "static serve without strip prefix",
            loggingcontract.Context{
                "path": requestPath,
            },
        )
    }

    receivedPath := requestPath
    if false == strings.HasPrefix(receivedPath, "/") {
        receivedPath = "/" + receivedPath
    }

    cleanedPath := path.Clean(receivedPath)

    if "." == cleanedPath || "" == cleanedPath {
        cleanedPath = "/"
    }

    if "/" == cleanedPath {
        /* spellings folding into the root are refused: the matchers compare the raw path; canonical is the mount root itself */
        canonicalRoot := strings.TrimSuffix(instance.config.stripPrefix, "/")

        if canonicalRoot != request.HttpRequest().URL.Path && canonicalRoot+"/" != request.HttpRequest().URL.Path {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          request.HttpRequest().URL.Path,
                    "canonicalPath": canonicalRoot + "/",
                },
            )

            return 0, nil, nil, false
        }

        cleanedPath = "/" + instance.config.indexFile

        /* the root resolves to the index only after the exclusion check, so an exclusion naming the index must fire here too */
        if true == hasExcludedPathPrefix(strings.TrimSuffix(instance.config.stripPrefix, "/")+cleanedPath, instance.config.excludedPathList) {
            logger.Debug(
                "static serve excluded path",
                loggingcontract.Context{
                    "path": requestPath,
                },
            )

            return 0, nil, nil, false
        }
    } else {
        /* a folded path is refused, not served or redirected: the matchers compare the raw path, so their rule would not fire */
        canonicalPath := strings.TrimSuffix(instance.config.stripPrefix, "/") + cleanedPath

        if canonicalPath != request.HttpRequest().URL.Path {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          request.HttpRequest().URL.Path,
                    "canonicalPath": canonicalPath,
                },
            )

            return 0, nil, nil, false
        }

        if true == hasDotPrefixedPathElement(cleanedPath, instance.config.allowedDotPrefixList) {
            logger.Warning(
                "static serve dot prefixed path element",
                loggingcontract.Context{
                    "cleanedPath": cleanedPath,
                },
            )

            return 0, nil, nil, false
        }
    }

    relativePath := strings.TrimPrefix(cleanedPath, "/")

    if ModeEmbedded == instance.config.mode && "" != instance.config.publicDir {
        relativePath = path.Join(instance.config.publicDir, relativePath)
    }

    logger.Debug(
        "static serve path resolved",
        loggingcontract.Context{
            "mode":         instance.config.mode,
            "cleanedPath":  cleanedPath,
            "relativePath": relativePath,
            "publicDir":    instance.config.publicDir,
        },
    )

    if false == fs.ValidPath(relativePath) {
        logger.Warning(
            "static serve invalid relative path",
            loggingcontract.Context{
                "relativePath": relativePath,
            },
        )

        return 0, nil, nil, false
    }

    file, err := instance.fileSystem.Open(relativePath)
    if nil != err {
        logOpenFailure(logger, relativePath, err)

        return 0, nil, nil, false
    }
    defer func() {
        _ = file.Close()
    }()

    fileInfo, err := file.Stat()
    if nil != err {
        logger.Debug(
            "static serve stat failed",
            exception.LogContext(
                err,
                exceptioncontract.Context{
                    "relativePath": relativePath,
                },
            ),
        )

        return 0, nil, nil, false
    }

    if true == fileInfo.IsDir() {
        logger.Info(
            "static serve target is directory",
            loggingcontract.Context{
                "relativePath": relativePath,
            },
        )

        return 0, nil, nil, false
    }

    headers := nethttp.Header{}

    extension := path.Ext(relativePath)
    if "" != extension {
        contentType := contentTypeByExtension(extension)
        if "" != contentType {
            headers.Set("Content-Type", contentType)
        }
    }

    if true == instance.config.enableCache {
        etag := instance.entityTag(relativePath, fileInfo)
        if "" != etag {
            headers.Set("ETag", etag)
        }

        /* a zero modification time is not a validator: it is never After anything, so every If-Modified-Since would answer 304 */
        if false == fileInfo.ModTime().IsZero() {
            lastModified := fileInfo.ModTime().UTC().Format(nethttp.TimeFormat)
            headers.Set("Last-Modified", lastModified)
        }

        cacheControl := buildCacheControlValue(instance.config.cacheMaxAge)
        if "" != cacheControl {
            headers.Set("Cache-Control", cacheControl)
        }

        ifNoneMatch := request.Header("If-None-Match")
        if true == EtagMatchesIfNoneMatch(ifNoneMatch, etag) {
            logger.Debug(
                "static serve 304 by etag",
                loggingcontract.Context{
                    "relativePath": relativePath,
                    "etag":         etag,
                },
            )

            return nethttp.StatusNotModified, headers, nil, true
        }

        /* the date is consulted only without an entity tag: a deploy preserving modification times would 304 stale caches */
        if "" == strings.TrimSpace(ifNoneMatch) && false == fileInfo.ModTime().IsZero() {
            ifModifiedSince := request.Header("If-Modified-Since")
            if "" != ifModifiedSince {
                if clientTime, parseErr := nethttp.ParseTime(ifModifiedSince); nil == parseErr {
                    modifiedAt := fileInfo.ModTime().UTC().Truncate(time.Second)

                    if false == modifiedAt.After(clientTime) {
                        logger.Debug(
                            "static serve 304 by last-modified",
                            loggingcontract.Context{
                                "relativePath":    relativePath,
                                "ifModifiedSince": ifModifiedSince,
                            },
                        )

                        return nethttp.StatusNotModified, headers, nil, true
                    }
                }
            }
        }
    }

    if nethttp.MethodHead == request.HttpRequest().Method {
        headers.Set("Content-Length", formatContentLength(fileInfo.Size()))

        logger.Debug(
            "static serve head success",
            loggingcontract.Context{
                "relativePath": relativePath,
                "size":         fileInfo.Size(),
                "contentType":  headers.Get("Content-Type"),
            },
        )

        return nethttp.StatusOK, headers, nil, true
    }

    content, err := io.ReadAll(file)
    if nil != err {
        logger.Error(
            "static serve read failed",
            exception.LogContext(
                err,
                exceptioncontract.Context{
                    "relativePath": relativePath,
                },
            ),
        )

        return nethttp.StatusInternalServerError, nil, nil, true
    }

    if 0 < fileInfo.Size() {
        headers.Set("Content-Length", formatContentLength(fileInfo.Size()))
    }

    logger.Debug(
        "static serve success",
        loggingcontract.Context{
            "relativePath": relativePath,
            "size":         len(content),
            "contentType":  headers.Get("Content-Type"),
        },
    )

    return nethttp.StatusOK, headers, content, true
}

func (instance *FileServer) serveForStreaming(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (int, nethttp.Header, fs.File, fs.FileInfo, bool) {
    logger = logging.EnsureLogger(logger)

    if true == internal.IsNilInterface(request) {
        return 0, nil, nil, nil, false
    }

    method := request.HttpRequest().Method

    if false == isRetrievalMethod(method) {
        /* debug, not info: with the middleware registered globally this fires for every POST and PUT */
        logger.Debug(
            "static serve method not eligible",
            loggingcontract.Context{
                "method": method,
            },
        )

        return 0, nil, nil, nil, false
    }

    requestPath := request.HttpRequest().URL.Path

    if true == hasExcludedPathPrefix(requestPath, instance.config.excludedPathList) {
        logger.Debug(
            "static serve excluded path",
            loggingcontract.Context{
                "path": requestPath,
            },
        )

        return 0, nil, nil, nil, false
    }

    if "" != instance.config.stripPrefix {
        if strings.HasPrefix(requestPath, instance.config.stripPrefix) {
            logger.Debug(
                "static serve strip prefix match",
                loggingcontract.Context{
                    "path":        requestPath,
                    "stripPrefix": instance.config.stripPrefix,
                },
            )

            requestPath = strings.TrimPrefix(requestPath, instance.config.stripPrefix)
            if "" == requestPath {
                requestPath = "/"
            }
        } else {
            /* debug, not info: every request outside the mounted prefix takes this exit */
            logger.Debug(
                "static serve strip prefix mismatch",
                loggingcontract.Context{
                    "path":        requestPath,
                    "stripPrefix": instance.config.stripPrefix,
                },
            )

            return 0, nil, nil, nil, false
        }
    } else {
        logger.Debug(
            "static serve without strip prefix",
            loggingcontract.Context{
                "path": requestPath,
            },
        )
    }

    receivedPath := requestPath
    if false == strings.HasPrefix(receivedPath, "/") {
        receivedPath = "/" + receivedPath
    }

    cleanedPath := path.Clean(receivedPath)

    if "." == cleanedPath || "" == cleanedPath {
        cleanedPath = "/"
    }

    if "/" == cleanedPath {
        /* spellings folding into the root are refused: the matchers compare the raw path; canonical is the mount root itself */
        canonicalRoot := strings.TrimSuffix(instance.config.stripPrefix, "/")

        if canonicalRoot != request.HttpRequest().URL.Path && canonicalRoot+"/" != request.HttpRequest().URL.Path {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          request.HttpRequest().URL.Path,
                    "canonicalPath": canonicalRoot + "/",
                },
            )

            return 0, nil, nil, nil, false
        }

        cleanedPath = "/" + instance.config.indexFile

        /* the root resolves to the index only after the exclusion check, so an exclusion naming the index must fire here too */
        if true == hasExcludedPathPrefix(strings.TrimSuffix(instance.config.stripPrefix, "/")+cleanedPath, instance.config.excludedPathList) {
            logger.Debug(
                "static serve excluded path",
                loggingcontract.Context{
                    "path": requestPath,
                },
            )

            return 0, nil, nil, nil, false
        }
    } else {
        /* a folded path is refused, not served or redirected: the matchers compare the raw path, so their rule would not fire */
        canonicalPath := strings.TrimSuffix(instance.config.stripPrefix, "/") + cleanedPath

        if canonicalPath != request.HttpRequest().URL.Path {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          request.HttpRequest().URL.Path,
                    "canonicalPath": canonicalPath,
                },
            )

            return 0, nil, nil, nil, false
        }

        if true == hasDotPrefixedPathElement(cleanedPath, instance.config.allowedDotPrefixList) {
            logger.Warning(
                "static serve dot prefixed path element",
                loggingcontract.Context{
                    "cleanedPath": cleanedPath,
                },
            )

            return 0, nil, nil, nil, false
        }
    }

    relativePath := strings.TrimPrefix(cleanedPath, "/")

    if ModeEmbedded == instance.config.mode && "" != instance.config.publicDir {
        relativePath = path.Join(instance.config.publicDir, relativePath)
    }

    logger.Debug(
        "static serve path resolved",
        loggingcontract.Context{
            "mode":         instance.config.mode,
            "cleanedPath":  cleanedPath,
            "relativePath": relativePath,
            "publicDir":    instance.config.publicDir,
        },
    )

    if false == fs.ValidPath(relativePath) {
        logger.Warning(
            "static serve invalid relative path",
            loggingcontract.Context{
                "relativePath": relativePath,
            },
        )

        return 0, nil, nil, nil, false
    }

    file, err := instance.fileSystem.Open(relativePath)
    if nil != err {
        logOpenFailure(logger, relativePath, err)

        return 0, nil, nil, nil, false
    }

    fileInfo, err := file.Stat()
    if nil != err {
        _ = file.Close()

        logger.Debug(
            "static serve stat failed",
            exception.LogContext(
                err,
                exceptioncontract.Context{
                    "relativePath": relativePath,
                },
            ),
        )

        return 0, nil, nil, nil, false
    }

    if true == fileInfo.IsDir() {
        _ = file.Close()

        logger.Info(
            "static serve target is directory",
            loggingcontract.Context{
                "relativePath": relativePath,
            },
        )

        return 0, nil, nil, nil, false
    }

    headers := nethttp.Header{}

    extension := path.Ext(relativePath)
    if "" != extension {
        contentType := contentTypeByExtension(extension)
        if "" != contentType {
            headers.Set("Content-Type", contentType)
        }
    }

    if true == instance.config.enableCache {
        etag := instance.entityTag(relativePath, fileInfo)
        if "" != etag {
            headers.Set("ETag", etag)
        }

        /* a zero modification time is not a validator: it is never After anything, so every If-Modified-Since would answer 304 */
        if false == fileInfo.ModTime().IsZero() {
            lastModified := fileInfo.ModTime().UTC().Format(nethttp.TimeFormat)
            headers.Set("Last-Modified", lastModified)
        }

        cacheControl := buildCacheControlValue(instance.config.cacheMaxAge)
        if "" != cacheControl {
            headers.Set("Cache-Control", cacheControl)
        }

        ifNoneMatch := request.Header("If-None-Match")
        if true == EtagMatchesIfNoneMatch(ifNoneMatch, etag) {
            logger.Debug(
                "static serve 304 by etag",
                loggingcontract.Context{
                    "relativePath": relativePath,
                    "etag":         etag,
                },
            )

            _ = file.Close()
            return nethttp.StatusNotModified, headers, nil, nil, true
        }

        /* the date is consulted only without an entity tag: a deploy preserving modification times would 304 stale caches */
        if "" == strings.TrimSpace(ifNoneMatch) && false == fileInfo.ModTime().IsZero() {
            ifModifiedSince := request.Header("If-Modified-Since")
            if "" != ifModifiedSince {
                if clientTime, parseErr := nethttp.ParseTime(ifModifiedSince); nil == parseErr {
                    modifiedAt := fileInfo.ModTime().UTC().Truncate(time.Second)

                    if false == modifiedAt.After(clientTime) {
                        logger.Debug(
                            "static serve 304 by last-modified",
                            loggingcontract.Context{
                                "relativePath":    relativePath,
                                "ifModifiedSince": ifModifiedSince,
                            },
                        )

                        _ = file.Close()
                        return nethttp.StatusNotModified, headers, nil, nil, true
                    }
                }
            }
        }
    }

    return nethttp.StatusOK, headers, file, fileInfo, true
}

func contentTypeByExtension(extension string) string {
    contentType := mime.TypeByExtension(extension)
    if "" != contentType {
        return contentType
    }

    return fallbackContentTypeByExtension[strings.ToLower(extension)]
}

var fallbackContentTypeByExtension = map[string]string{
    ".css":   "text/css; charset=utf-8",
    ".ico":   "image/x-icon",
    ".js":    "text/javascript; charset=utf-8",
    ".json":  "application/json",
    ".map":   "application/json",
    ".mjs":   "text/javascript; charset=utf-8",
    ".otf":   "font/otf",
    ".svg":   "image/svg+xml",
    ".ttf":   "font/ttf",
    ".wasm":  "application/wasm",
    ".webp":  "image/webp",
    ".woff":  "font/woff",
    ".woff2": "font/woff2",
}

/* logOpenFailure separates a refusal from a miss: a miss is the ordinary case, at debug; a permission error is not */
func logOpenFailure(logger loggingcontract.Logger, relativePath string, openErr error) {
    logContext := exception.LogContext(
        openErr,
        exceptioncontract.Context{
            "relativePath": relativePath,
        },
    )

    var pathErr *fs.PathError
    if true == exception.ChainAs(openErr, &pathErr) && true == exception.ChainIs(openErr, fs.ErrPermission) {
        logger.Warning("static serve could not open a path the operating system refused", logContext)

        return
    }

    if true == exception.ChainIs(openErr, fs.ErrPermission) {
        logger.Warning("static serve refused a path that resolves outside the served directory", logContext)

        return
    }

    logger.Debug("static serve open failed", logContext)
}
