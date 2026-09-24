package static

import (
    "errors"
    "fmt"
    "io"
    "io/fs"
    "mime"
    nethttp "net/http"
    "path"
    "path/filepath"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type FileServer struct {
    config     *FileServerConfig
    fileSystem fs.FS
}

func NewFileServer(options *Options) *FileServer {
    /* nil options are refused by name, since the default would be a live file server over the "public" directory */
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

    /* in the embedded mode the public directory must exist inside the embedded filesystem, so a directory the build did not embed is refused at construction rather than answering 404 for every asset */
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

    /* the configuration is copied, struct and both lists, so the server is immutable once built: requests read these fields with no lock */
    configCopy := *options.fileServerConfig
    configCopy.allowedDotPrefixList = append([]string{}, options.fileServerConfig.allowedDotPrefixList...)
    configCopy.excludedPathList = append([]string{}, options.fileServerConfig.excludedPathList...)
    config := &configCopy

    if "" == config.indexFile {
        config.indexFile = "index.html"
    }

    /* an explicit zero is honoured as max-age=0; only a negative value takes the default */
    if true == config.enableCache && 0 > config.cacheMaxAge {
        config.cacheMaxAge = 3600
    }

    return &FileServer{
        config:     config,
        fileSystem: fileSystem,
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

type resolvedFile struct {
    relativePath string
    file         fs.File
    fileInfo     fs.FileInfo
    headers      nethttp.Header
    notModified  bool
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

    resolved, ok := instance.resolveAndOpen(request, logger)
    if false == ok {
        return 0, nil, nil, false
    }

    if true == resolved.notModified {
        return nethttp.StatusNotModified, resolved.headers, nil, true
    }

    defer func() {
        if nil != resolved.file {
            _ = resolved.file.Close()
        }
    }()

    if nethttp.MethodHead == request.HttpRequest().Method {
        resolved.headers.Set("Content-Length", formatContentLength(resolved.fileInfo.Size()))

        logger.Debug(
            "static serve head success",
            loggingcontract.Context{
                "relativePath": resolved.relativePath,
                "size":         resolved.fileInfo.Size(),
                "contentType":  resolved.headers.Get("Content-Type"),
            },
        )

        return nethttp.StatusOK, resolved.headers, nil, true
    }

    content, readErr := io.ReadAll(resolved.file)
    if nil != readErr {
        logger.Error(
            "static serve read failed",
            exception.LogContext(
                readErr,
                exceptioncontract.Context{
                    "relativePath": resolved.relativePath,
                },
            ),
        )

        return nethttp.StatusInternalServerError, nil, nil, true
    }

    if 0 < resolved.fileInfo.Size() {
        resolved.headers.Set("Content-Length", formatContentLength(resolved.fileInfo.Size()))
    }

    logger.Debug(
        "static serve success",
        loggingcontract.Context{
            "relativePath": resolved.relativePath,
            "size":         len(content),
            "contentType":  resolved.headers.Get("Content-Type"),
        },
    )

    return nethttp.StatusOK, resolved.headers, content, true
}

func (instance *FileServer) resolveAndOpen(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (*resolvedFile, bool) {
    method := request.HttpRequest().Method

    if false == isRetrievalMethod(method) {
        /* debug, since with the middleware registered globally this fires for every POST and PUT */
        logger.Debug(
            "static serve method not eligible",
            loggingcontract.Context{
                "method": method,
            },
        )

        return nil, false
    }

    /* the spelling the router matched, not the decoded URL.Path, so an encoded separator names a file whose name carries "%2F" rather than a file under another prefix the access-control matcher never weighed */
    routedPath := melodyhttp.RequestPathAsRouted(internal.RequestPathAsSent(request.HttpRequest().URL))
    requestPath := routedPath

    if true == hasExcludedPathPrefix(requestPath, instance.config.excludedPathList) {
        logger.Debug(
            "static serve excluded path",
            loggingcontract.Context{
                "path": requestPath,
            },
        )

        return nil, false
    }

    if "" != instance.config.stripPrefix {
        if true == strings.HasPrefix(requestPath, instance.config.stripPrefix) {
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
            /* debug, since every request outside the mounted prefix takes this exit */
            logger.Debug(
                "static serve strip prefix mismatch",
                loggingcontract.Context{
                    "path":        requestPath,
                    "stripPrefix": instance.config.stripPrefix,
                },
            )

            return nil, false
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
        /* a spelling that folds into the root is refused, since the matchers in front compare the raw path and "/open/.." would serve the mount's index page from behind another prefix's rule; the mount root itself, with or without its trailing slash, is canonical */
        canonicalRoot := strings.TrimSuffix(instance.config.stripPrefix, "/")

        if canonicalRoot != routedPath && canonicalRoot+"/" != routedPath {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          routedPath,
                    "canonicalPath": canonicalRoot + "/",
                },
            )

            return nil, false
        }

        /* the mount root answers with the configured index file */
        cleanedPath = "/" + instance.config.indexFile

        /* the exclusion list is consulted again for the resolved index file, so an exclusion naming it fires for "/" too */
        if true == hasExcludedPathPrefix(strings.TrimSuffix(instance.config.stripPrefix, "/")+cleanedPath, instance.config.excludedPathList) {
            logger.Debug(
                "static serve excluded path",
                loggingcontract.Context{
                    "path": requestPath,
                },
            )

            return nil, false
        }
    } else {
        /* the file must sit at exactly the path received: the matchers in front compare the raw path, so serving a folded target would put the file behind a url access control never saw, and a redirect would teach that url. The comparison rebuilds the whole path around the configured strip prefix, so a doubled slash at the boundary is not absorbed. */
        canonicalPath := strings.TrimSuffix(instance.config.stripPrefix, "/") + cleanedPath

        if canonicalPath != routedPath {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          routedPath,
                    "canonicalPath": canonicalPath,
                },
            )

            return nil, false
        }

        if true == hasDotPrefixedPathElement(cleanedPath, instance.config.allowedDotPrefixList) {
            logger.Warning(
                "static serve dot prefixed path element",
                loggingcontract.Context{
                    "cleanedPath": cleanedPath,
                },
            )

            return nil, false
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

        return nil, false
    }

    file, openErr := instance.fileSystem.Open(relativePath)
    if nil != openErr {
        logOpenFailure(logger, relativePath, openErr)

        return nil, false
    }

    fileInfo, statErr := file.Stat()
    if nil != statErr {
        _ = file.Close()

        logger.Debug(
            "static serve stat failed",
            exception.LogContext(
                statErr,
                exceptioncontract.Context{
                    "relativePath": relativePath,
                },
            ),
        )

        return nil, false
    }

    if true == fileInfo.IsDir() {
        _ = file.Close()

        logger.Info(
            "static serve target is directory",
            loggingcontract.Context{
                "relativePath": relativePath,
            },
        )

        return nil, false
    }

    /* only a regular file is served, read off the opened handle: a FIFO would park the goroutine and a device or socket is not a file's bytes */
    if false == fileInfo.Mode().IsRegular() {
        _ = file.Close()

        logger.Info(
            "static serve target is not a regular file",
            loggingcontract.Context{
                "relativePath": relativePath,
                "mode":         fileInfo.Mode().String(),
            },
        )

        return nil, false
    }

    headers := nethttp.Header{}

    extension := path.Ext(relativePath)
    if "" != extension {
        contentType := contentTypeByExtension(extension)
        if "" != contentType {
            headers.Set("Content-Type", contentType)
        }
    }

    notModified := false

    if true == instance.config.enableCache {
        etag := GenerateEtag(fileInfo, instance.config.weakEtag)
        if "" != etag {
            headers.Set("ETag", etag)
        }

        /* a filesystem with no modification time reports the zero instant, so no Last-Modified is sent and the entity tag is the only validator */
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

            return &resolvedFile{
                relativePath: relativePath,
                file:         nil,
                fileInfo:     fileInfo,
                headers:      headers,
                notModified:  true,
            }, true
        }

        /* the modification date is consulted only when no entity tag was offered: a client that sent one stated which bytes it holds, and a deploy may keep modification times while changing content */
        if "" == strings.TrimSpace(ifNoneMatch) && false == fileInfo.ModTime().IsZero() {
            ifModifiedSince := request.Header("If-Modified-Since")
            if "" != ifModifiedSince {
                /* an HTTP date may take any of three formats */
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

                        return &resolvedFile{
                            relativePath: relativePath,
                            file:         nil,
                            fileInfo:     fileInfo,
                            headers:      headers,
                            notModified:  true,
                        }, true
                    }
                }
            }
        }
    }

    return &resolvedFile{
        relativePath: relativePath,
        file:         file,
        fileInfo:     fileInfo,
        headers:      headers,
        notModified:  notModified,
    }, true
}

/* the streaming resolution logs as the buffered one does, since every static byte served is resolved here */
func (instance *FileServer) serveForStreaming(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (int, nethttp.Header, fs.File, fs.FileInfo, bool) {
    logger = logging.EnsureLogger(logger)

    if true == internal.IsNilInterface(request) {
        return 0, nil, nil, nil, false
    }

    resolved, ok := instance.resolveAndOpen(request, logger)
    if false == ok {
        return 0, nil, nil, nil, false
    }

    if true == resolved.notModified {
        return nethttp.StatusNotModified, resolved.headers, nil, nil, true
    }

    return nethttp.StatusOK, resolved.headers, resolved.file, resolved.fileInfo, true
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

/* logOpenFailure records an ordinary miss at debug, since the static server is consulted for every unrouted request, and a permission error at warning: here it comes from the containment guards of dirFileSystem.Open. */
func logOpenFailure(logger loggingcontract.Logger, relativePath string, openErr error) {
    logContext := exception.LogContext(
        openErr,
        exceptioncontract.Context{
            "relativePath": relativePath,
        },
    )

    if true == errors.Is(openErr, fs.ErrPermission) {
        logger.Warning("static serve refused a path that resolves outside the served directory", logContext)

        return
    }

    logger.Debug("static serve open failed", logContext)
}
