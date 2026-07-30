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

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    httpcontract "github.com/precision-soft/melody/http/contract"
    "github.com/precision-soft/melody/logging"
    loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type FileServer struct {
    config     *FileServerConfig
    fileSystem fs.FS
}

func NewFileServer(options *Options) *FileServer {
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

    config := options.fileServerConfig

    if "" == config.indexFile {
        config.indexFile = "index.html"
    }

    enableCache := config.enableCache
    cacheMaxAge := config.cacheMaxAge
    if true == enableCache && 0 >= cacheMaxAge {
        cacheMaxAge = 3600
        config.cacheMaxAge = cacheMaxAge
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

func (instance *FileServer) Serve(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (int, nethttp.Header, []byte, bool) {
    logger = logging.EnsureLogger(logger)

    if nil == request {
        logger.Warning("static serve skipped because request is nil", nil)

        return 0, nil, nil, false
    }

    method := request.HttpRequest().Method

    if false == isRetrievalMethod(method) {
        logger.Info(
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
            logger.Info(
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
        /* the mount root answers with the configured index file, and keeps answering it for the spellings that fold into the root, because that page is what a browser asks for by visiting the site. The index file is named by configuration and never by the request, so this resolution cannot be aimed at another file. */
        cleanedPath = "/" + instance.config.indexFile

        /* the exclusion list was consulted with the spelling the client sent, and the root resolves to the index file only after that consultation: an exclusion naming the index file must fire for the resolved spelling too, or "/" would serve off the disk the very page the operator handed to the application */
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
        etag := GenerateEtag(fileInfo, instance.config.weakEtag)
        if "" != etag {
            headers.Set("ETag", etag)
        }

        lastModified := fileInfo.ModTime().UTC().Format(nethttp.TimeFormat)
        headers.Set("Last-Modified", lastModified)

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

        /* the modification date is only consulted when no entity tag was offered: a client that sent one has already stated which bytes it holds, and the tag is the accurate answer to that question. Consulting the date as well turns a deploy that rewrites content while preserving modification times — a checkout, a rsync with --times, a container image rebuild — into a 304 for every cache that just proved, by offering a tag that does not match, that it holds different bytes. */
        if "" == strings.TrimSpace(ifNoneMatch) {
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

/* the streaming resolution carries the same log record as the buffered one: every static byte a running application serves is resolved here, so a refusal that stays silent — a traversal attempt, a symlink escape, a path the file system rejects — leaves the only trace of the attempt nowhere */
func (instance *FileServer) serveForStreaming(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (int, nethttp.Header, fs.File, fs.FileInfo, bool) {
    logger = logging.EnsureLogger(logger)

    if nil == request {
        return 0, nil, nil, nil, false
    }

    method := request.HttpRequest().Method

    if false == isRetrievalMethod(method) {
        logger.Info(
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
            logger.Info(
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
        /* the mount root answers with the configured index file, and keeps answering it for the spellings that fold into the root, because that page is what a browser asks for by visiting the site. The index file is named by configuration and never by the request, so this resolution cannot be aimed at another file. */
        cleanedPath = "/" + instance.config.indexFile

        /* the exclusion list was consulted with the spelling the client sent, and the root resolves to the index file only after that consultation: an exclusion naming the index file must fire for the resolved spelling too, or "/" would serve off the disk the very page the operator handed to the application */
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
        etag := GenerateEtag(fileInfo, instance.config.weakEtag)
        if "" != etag {
            headers.Set("ETag", etag)
        }

        lastModified := fileInfo.ModTime().UTC().Format(nethttp.TimeFormat)
        headers.Set("Last-Modified", lastModified)

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

        /* the modification date is only consulted when no entity tag was offered: a client that sent one has already stated which bytes it holds, and the tag is the accurate answer to that question. Consulting the date as well turns a deploy that rewrites content while preserving modification times — a checkout, a rsync with --times, a container image rebuild — into a 304 for every cache that just proved, by offering a tag that does not match, that it holds different bytes. */
        if "" == strings.TrimSpace(ifNoneMatch) {
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

/* logOpenFailure separates a refusal from a miss, which the level is the only thing that can say. The static server is consulted for every request a route did not answer, so a path that simply names no file is the ordinary case and is recorded at debug along with the successful resolutions; anything louder files one record per request that is not a static asset, and an operator learns to filter the whole message out.

A permission error is not that case. The only thing that produces one here is the escape check in resolveAndOpen: a path whose symlinks resolve outside the base directory. Recorded at debug it is byte-identical to a typo in a stylesheet href, which is exactly the indistinguishability the logging on this path exists to end. */
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
