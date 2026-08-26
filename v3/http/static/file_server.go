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
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
)

type FileServer struct {
    config     *FileServerConfig
    fileSystem fs.FS
}

func NewFileServer(options *Options) *FileServer {
    /* nil options are refused by name rather than read as a default: every sibling door that normalizes an absent configuration falls back to inert defaults, but the default here would be a live file server over the "public" directory — a nil that is almost always a wiring mistake would start serving files nobody asked served. */
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

    /* in the embedded mode the public directory is a path INSIDE a filesystem whose layout was frozen at compile time, while the value naming it stays a runtime key: MELODY_PUBLIC_DIR set to a directory the build did not embed passed every validation, booted, and then answered 404 for every asset the binary carries. The directory is proven here instead, because a public directory that does not exist is a wiring fault of the deployment and the alternative — ignoring the key in this mode — would dissolve the join that confines a stripped prefix to it. */
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

    /* the configuration is copied here — struct and both lists — so the server is immutable once built: the defaults below land on the copy instead of being written into the caller's struct, and a setter called after construction configures the next server rather than racing the in-flight requests of this one, which read these fields with no lock. */
    configCopy := *options.fileServerConfig
    configCopy.allowedDotPrefixList = append([]string{}, options.fileServerConfig.allowedDotPrefixList...)
    configCopy.excludedPathList = append([]string{}, options.fileServerConfig.excludedPathList...)
    config := &configCopy

    if "" == config.indexFile {
        config.indexFile = "index.html"
    }

    /* an explicit zero is honoured as max-age=0 — always revalidate, with the ETag and Last-Modified machinery intact — because the configuration door validates zero as a distinct choice; only a negative value reads as unset and takes the default. Coercing zero shipped an hour of freshness to the operator who asked for none. */
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

    if nil == request {
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

    if nil == request {
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
        /* debug, not info: with the middleware registered globally this fires for every POST and PUT in the application — the per-request noise the logging comment on logOpenFailure exists to keep out of the journal */
        logger.Debug(
            "static serve method not eligible",
            loggingcontract.Context{
                "method": method,
            },
        )

        return nil, false
    }

    requestPath := request.HttpRequest().URL.Path

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
            /* debug, not info: every request outside the mounted prefix takes this exit, so anything louder files one record per ordinary api request and the operator filters the message out — the reasoning logOpenFailure states for the ordinary miss */
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
        /* the spellings that fold into the root are refused on the ground the branch below states and until now alone carried: the matchers in front of the application compare the raw path, so "/open/.." is a url no rule on this mount ever saw, and answering it serves the mount's index page from behind whatever rule that other prefix carries. The index file is named by configuration and never by the request, so the target cannot be aimed elsewhere — the exposure of that one page can. Canonical is the mount root itself, with or without its trailing slash. */
        canonicalRoot := strings.TrimSuffix(instance.config.stripPrefix, "/")

        if canonicalRoot != request.HttpRequest().URL.Path && canonicalRoot+"/" != request.HttpRequest().URL.Path {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          request.HttpRequest().URL.Path,
                    "canonicalPath": canonicalRoot + "/",
                },
            )

            return nil, false
        }

        /* the mount root answers with the configured index file, because that page is what a browser asks for by visiting the site */
        cleanedPath = "/" + instance.config.indexFile

        /* the exclusion list was consulted with the spelling the client sent, and the root resolves to the index file only after that consultation: an exclusion naming the index file must fire for the resolved spelling too, or "/" would serve off the disk the very page the operator handed to the application */
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
        /* the file has to sit at exactly the path that was received. path.Clean folds "..", "//", "/./" and a trailing slash away, and serving the folded target under the received spelling puts the file behind a URL access control never saw: the matchers in front of the application compare the raw path, so a rule on "/internal/" does not fire for "/open/../internal/secret.json". A refusal is the only answer that keeps the two views of the request in agreement — a redirect would still teach the client a spelling that reaches the file while sidestepping the rule. The strip prefix is configuration rather than client input, so the comparison rebuilds the whole path around it: comparing only the remainder would let a doubled slash at the prefix boundary be absorbed by the strip and pass unnoticed. */
        canonicalPath := strings.TrimSuffix(instance.config.stripPrefix, "/") + cleanedPath

        if canonicalPath != request.HttpRequest().URL.Path {
            logger.Warning(
                "static serve non canonical path",
                loggingcontract.Context{
                    "path":          request.HttpRequest().URL.Path,
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

    /* only a regular file is served: a FIFO in the public directory blocks the reading goroutine for as long as nobody writes the other end — one request parks a goroutine forever, and a handful park a handful — while a device node or a socket answers bytes that are not a file's. The mode is read off the handle already opened, so the answer describes what was opened, not what a second look finds. */
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

        /* a filesystem that carries no modification time reports the zero instant, and rendering it as "Mon, 01 Jan 0001 00:00:00 GMT" publishes a validator that is not one: the zero time is never After anything, so every conditional request carrying If-Modified-Since and no entity tag was answered 304 for the life of the deployment. An absent header states what is true — this filesystem cannot date its files — and leaves the entity tag as the only validator, which is where the build version already answers. */
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

        /* the modification date is only consulted when no entity tag was offered: a client that sent one has already stated which bytes it holds, and the tag is the accurate answer to that question. Consulting the date as well turns a deploy that rewrites content while preserving modification times — a checkout, a rsync with --times, a container image rebuild — into a 304 for every cache that just proved, by offering a tag that does not match, that it holds different bytes. */
        if "" == strings.TrimSpace(ifNoneMatch) && false == fileInfo.ModTime().IsZero() {
            ifModifiedSince := request.Header("If-Modified-Since")
            if "" != ifModifiedSince {
                /* the field carries any of the three date formats an HTTP date may take, and only one of them is nethttp.TimeFormat; parsing that one alone silently re-sends the whole body to a client whose cache writes asctime or the RFC 850 form */
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

/* the streaming resolution carries the same log record as the buffered one: every static byte a running application serves is resolved here, so a refusal that stays silent — a traversal attempt, a symlink escape, a path the file system rejects — leaves the only trace of the attempt nowhere */
func (instance *FileServer) serveForStreaming(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (int, nethttp.Header, fs.File, fs.FileInfo, bool) {
    logger = logging.EnsureLogger(logger)

    if nil == request {
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

/* logOpenFailure separates a refusal from a miss, which the level is the only thing that can say. The static server is consulted for every request a route did not answer, so a path that simply names no file is the ordinary case and is recorded at debug along with the successful resolutions; anything louder files one record per request that is not a static asset, and an operator learns to filter the whole message out.

   A permission error is not that case. What produces one here are the two containment guards of dirFileSystem.Open — the dot-dot prefix refusal and the check that a path's symlinks resolve inside the base directory — and in the embedded mode neither can fire. Recorded at debug it is byte-identical to a typo in a stylesheet href, which is exactly the indistinguishability the logging on this path exists to end. */
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
