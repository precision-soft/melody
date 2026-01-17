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

func (instance *FileServer) Serve(
	request httpcontract.Request,
	logger loggingcontract.Logger,
) (int, nethttp.Header, []byte, bool) {
	logger = logging.EnsureLogger(logger)

	if nil == request {
		logger.Warning("static serve skipped because request is nil", nil)

		return 0, nil, nil, false
	}

	requestPath := request.HttpRequest().URL.Path

	if "" != instance.config.stripPrefix {
		if strings.HasPrefix(requestPath, instance.config.stripPrefix) {
			logger.Info(
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
		logger.Info(
			"static serve without strip prefix",
			loggingcontract.Context{
				"path": requestPath,
			},
		)
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

	logger.Info(
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
		logger.Error(
			"static serve open failed",
			exception.LogContext(
				err,
				exceptioncontract.Context{
					"relativePath": relativePath,
				},
			),
		)

		return 0, nil, nil, false
	}
	defer func() {
		_ = file.Close()
	}()

	fileInfo, err := file.Stat()
	if nil != err {
		logger.Error(
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
		contentType := mime.TypeByExtension(extension)
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
		if "" != ifNoneMatch && ifNoneMatch == etag {
			logger.Info(
				"static serve 304 by etag",
				loggingcontract.Context{
					"relativePath": relativePath,
					"etag":         etag,
				},
			)

			return nethttp.StatusNotModified, headers, nil, true
		}

		ifModifiedSince := request.Header("If-Modified-Since")
		if "" != ifModifiedSince {
			if clientTime, err := time.Parse(nethttp.TimeFormat, ifModifiedSince); nil == err {
				modifiedAt := fileInfo.ModTime().UTC().Truncate(time.Second)

				if false == modifiedAt.After(clientTime) {
					logger.Info(
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

	if nethttp.MethodHead == request.HttpRequest().Method {
		headers.Set("Content-Length", formatContentLength(fileInfo.Size()))

		logger.Info(
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

	logger.Info(
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
) (int, nethttp.Header, fs.File, fs.FileInfo, bool) {
	if nil == request {
		return 0, nil, nil, nil, false
	}

	requestPath := request.HttpRequest().URL.Path

	if "" != instance.config.stripPrefix {
		if strings.HasPrefix(requestPath, instance.config.stripPrefix) {
			requestPath = strings.TrimPrefix(requestPath, instance.config.stripPrefix)
			if "" == requestPath {
				requestPath = "/"
			}
		} else {
			return 0, nil, nil, nil, false
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

	if false == fs.ValidPath(relativePath) {
		return 0, nil, nil, nil, false
	}

	file, err := instance.fileSystem.Open(relativePath)
	if nil != err {
		return 0, nil, nil, nil, false
	}

	fileInfo, err := file.Stat()
	if nil != err {
		_ = file.Close()
		return 0, nil, nil, nil, false
	}

	if true == fileInfo.IsDir() {
		_ = file.Close()
		return 0, nil, nil, nil, false
	}

	headers := nethttp.Header{}

	extension := path.Ext(relativePath)
	if "" != extension {
		contentType := mime.TypeByExtension(extension)
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
		if "" != ifNoneMatch && ifNoneMatch == etag {
			_ = file.Close()
			return nethttp.StatusNotModified, headers, nil, nil, true
		}

		ifModifiedSince := request.Header("If-Modified-Since")
		if "" != ifModifiedSince {
			if clientTime, err := time.Parse(nethttp.TimeFormat, ifModifiedSince); nil == err {
				modifiedAt := fileInfo.ModTime().UTC().Truncate(time.Second)

				if false == modifiedAt.After(clientTime) {
					_ = file.Close()
					return nethttp.StatusNotModified, headers, nil, nil, true
				}
			}
		}
	}

	return nethttp.StatusOK, headers, file, fileInfo, true
}
