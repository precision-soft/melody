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

    statusCode, headers, file, fileInfo, ok := instance.serveForStreaming(request)
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

func (instance *FileServer) resolveAndOpen(
    request httpcontract.Request,
    logger loggingcontract.Logger,
) (*resolvedFile, bool) {
    requestPath := request.HttpRequest().URL.Path

    if "" != instance.config.stripPrefix {
        if true == strings.HasPrefix(requestPath, instance.config.stripPrefix) {
            if nil != logger {
                logger.Info(
                    "static serve strip prefix match",
                    loggingcontract.Context{
                        "path":        requestPath,
                        "stripPrefix": instance.config.stripPrefix,
                    },
                )
            }

            requestPath = strings.TrimPrefix(requestPath, instance.config.stripPrefix)
            if "" == requestPath {
                requestPath = "/"
            }
        } else {
            if nil != logger {
                logger.Info(
                    "static serve strip prefix mismatch",
                    loggingcontract.Context{
                        "path":        requestPath,
                        "stripPrefix": instance.config.stripPrefix,
                    },
                )
            }

            return nil, false
        }
    } else {
        if nil != logger {
            logger.Info(
                "static serve without strip prefix",
                loggingcontract.Context{
                    "path": requestPath,
                },
            )
        }
    }

    cleanedPath := path.Clean(requestPath)
    if "." == cleanedPath || "" == cleanedPath {
        cleanedPath = "/"
    }

    if "/" == cleanedPath {
        cleanedPath = "/" + instance.config.indexFile
    }

    relativePath := strings.TrimPrefix(cleanedPath, "/")

    if ModeEmbedded == instance.config.mode && "" != instance.config.publicDir {
        relativePath = path.Join(instance.config.publicDir, relativePath)
    }

    if nil != logger {
        logger.Info(
            "static serve path resolved",
            loggingcontract.Context{
                "mode":         instance.config.mode,
                "cleanedPath":  cleanedPath,
                "relativePath": relativePath,
                "publicDir":    instance.config.publicDir,
            },
        )
    }

    if false == fs.ValidPath(relativePath) {
        if nil != logger {
            logger.Warning(
                "static serve invalid relative path",
                loggingcontract.Context{
                    "relativePath": relativePath,
                },
            )
        }

        return nil, false
    }

    file, openErr := instance.fileSystem.Open(relativePath)
    if nil != openErr {
        if nil != logger {
            logger.Error(
                "static serve open failed",
                exception.LogContext(
                    openErr,
                    exceptioncontract.Context{
                        "relativePath": relativePath,
                    },
                ),
            )
        }

        return nil, false
    }

    fileInfo, statErr := file.Stat()
    if nil != statErr {
        _ = file.Close()

        if nil != logger {
            logger.Error(
                "static serve stat failed",
                exception.LogContext(
                    statErr,
                    exceptioncontract.Context{
                        "relativePath": relativePath,
                    },
                ),
            )
        }

        return nil, false
    }

    if true == fileInfo.IsDir() {
        _ = file.Close()

        if nil != logger {
            logger.Info(
                "static serve target is directory",
                loggingcontract.Context{
                    "relativePath": relativePath,
                },
            )
        }

        return nil, false
    }

    headers := nethttp.Header{}

    extension := path.Ext(relativePath)
    if "" != extension {
        contentType := mime.TypeByExtension(extension)
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

        lastModified := fileInfo.ModTime().UTC().Format(nethttp.TimeFormat)
        headers.Set("Last-Modified", lastModified)

        cacheControl := buildCacheControlValue(instance.config.cacheMaxAge)
        if "" != cacheControl {
            headers.Set("Cache-Control", cacheControl)
        }

        ifNoneMatch := request.Header("If-None-Match")
        if "" != ifNoneMatch && ifNoneMatch == etag {
            if nil != logger {
                logger.Info(
                    "static serve 304 by etag",
                    loggingcontract.Context{
                        "relativePath": relativePath,
                        "etag":         etag,
                    },
                )
            }

            _ = file.Close()

            return &resolvedFile{
                relativePath: relativePath,
                file:         nil,
                fileInfo:     fileInfo,
                headers:      headers,
                notModified:  true,
            }, true
        }

        ifModifiedSince := request.Header("If-Modified-Since")
        if "" != ifModifiedSince {
            if clientTime, parseErr := time.Parse(nethttp.TimeFormat, ifModifiedSince); nil == parseErr {
                modifiedAt := fileInfo.ModTime().UTC().Truncate(time.Second)

                if false == modifiedAt.After(clientTime) {
                    if nil != logger {
                        logger.Info(
                            "static serve 304 by last-modified",
                            loggingcontract.Context{
                                "relativePath":    relativePath,
                                "ifModifiedSince": ifModifiedSince,
                            },
                        )
                    }

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

    return &resolvedFile{
        relativePath: relativePath,
        file:         file,
        fileInfo:     fileInfo,
        headers:      headers,
        notModified:  notModified,
    }, true
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

        logger.Info(
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

    logger.Info(
        "static serve success",
        loggingcontract.Context{
            "relativePath": resolved.relativePath,
            "size":         len(content),
            "contentType":  resolved.headers.Get("Content-Type"),
        },
    )

    return nethttp.StatusOK, resolved.headers, content, true
}

func (instance *FileServer) serveForStreaming(
    request httpcontract.Request,
) (int, nethttp.Header, fs.File, fs.FileInfo, bool) {
    if nil == request {
        return 0, nil, nil, nil, false
    }

    resolved, ok := instance.resolveAndOpen(request, nil)
    if false == ok {
        return 0, nil, nil, nil, false
    }

    if true == resolved.notModified {
        return nethttp.StatusNotModified, resolved.headers, nil, nil, true
    }

    return nethttp.StatusOK, resolved.headers, resolved.file, resolved.fileInfo, true
}
