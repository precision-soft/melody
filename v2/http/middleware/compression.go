package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	nethttp "net/http"
	"strconv"
	"strings"

	httpcontract "github.com/precision-soft/melody/v2/http/contract"
	runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

type CompressionConfig struct {
	level                int
	minSize              int
	excludedContentTypes []string
	excludedPaths        []string
}

func NewCompressionConfig(
	level int,
	minSize int,
	excludedContentTypes []string,
	excludedPaths []string,
) *CompressionConfig {
	var copiedExcludedContentTypes []string
	if nil != excludedContentTypes {
		copiedExcludedContentTypes = append([]string{}, excludedContentTypes...)
	}

	var copiedExcludedPaths []string
	if nil != excludedPaths {
		copiedExcludedPaths = append([]string{}, excludedPaths...)
	}

	return &CompressionConfig{
		level:                level,
		minSize:              minSize,
		excludedContentTypes: copiedExcludedContentTypes,
		excludedPaths:        copiedExcludedPaths,
	}
}

func (instance *CompressionConfig) Level() int { return instance.level }

func (instance *CompressionConfig) SetLevel(level int) { instance.level = level }

func (instance *CompressionConfig) MinSize() int { return instance.minSize }

func (instance *CompressionConfig) SetMinSize(minSize int) { instance.minSize = minSize }

func (instance *CompressionConfig) ExcludedContentTypes() []string {
	if nil == instance.excludedContentTypes {
		return nil
	}

	return append([]string{}, instance.excludedContentTypes...)
}

func (instance *CompressionConfig) SetExcludedContentTypes(excludedContentTypes []string) {
	if nil == excludedContentTypes {
		instance.excludedContentTypes = nil
		return
	}

	instance.excludedContentTypes = append([]string{}, excludedContentTypes...)
}

func (instance *CompressionConfig) ExcludedPaths() []string {
	if nil == instance.excludedPaths {
		return nil
	}

	return append([]string{}, instance.excludedPaths...)
}

func (instance *CompressionConfig) SetExcludedPaths(excludedPaths []string) {
	if nil == excludedPaths {
		instance.excludedPaths = nil
		return
	}

	instance.excludedPaths = append([]string{}, excludedPaths...)
}

func DefaultCompressionConfig() *CompressionConfig {
	return NewCompressionConfig(
		gzip.DefaultCompression,
		1024,
		[]string{
			"image/",
			"video/",
			"audio/",
			"application/zip",
			"application/gzip",
			"application/x-gzip",
		},
		nil,
	)
}

func CompressionMiddleware(config *CompressionConfig) httpcontract.Middleware {
	if 0 == config.Level() {
		config.SetLevel(gzip.DefaultCompression)
	}

	if 0 == config.MinSize() {
		config.SetMinSize(1024)
	}

	return func(next httpcontract.Handler) httpcontract.Handler {
		return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
			response, nextMiddlewareErr := next(runtimeInstance, writer, request)
			if nil != nextMiddlewareErr || nil == response {
				return response, nextMiddlewareErr
			}

			httpRequest := request.HttpRequest()
			if nil == httpRequest {
				return response, nil
			}

			for _, excludedPath := range config.ExcludedPaths() {
				if "" != excludedPath && true == strings.HasPrefix(httpRequest.URL.Path, excludedPath) {
					return response, nil
				}
			}

			acceptEncoding := httpRequest.Header.Get("Accept-Encoding")
			if false == strings.Contains(acceptEncoding, "gzip") {
				return response, nil
			}

			if nil == response.BodyReader() {
				return response, nil
			}

			if "" != response.Headers().Get("Content-Encoding") {
				return response, nil
			}

			contentType := response.Headers().Get("Content-Type")
			for _, excludedContentType := range config.ExcludedContentTypes() {
				if "" != excludedContentType && true == strings.HasPrefix(contentType, excludedContentType) {
					return response, nil
				}
			}

			contentLengthString := response.Headers().Get("Content-Length")
			if "" != contentLengthString {
				value, parseErr := strconv.Atoi(contentLengthString)
				if nil == parseErr && config.MinSize() > value {
					return response, nil
				}
			}

			originalReader := response.BodyReader()
			if closer, ok := originalReader.(io.Closer); true == ok {
				defer func(closer io.Closer) { _ = closer.Close() }(closer)
			}

			data, readErr := io.ReadAll(originalReader)
			if nil != readErr {
				return response, nil
			}

			if config.MinSize() > len(data) {
				response.SetBodyReader(bytes.NewReader(data))
				return response, nil
			}

			var buffer bytes.Buffer
			gzipWriter, gzipErr := gzip.NewWriterLevel(&buffer, config.Level())
			if nil != gzipErr {
				response.SetBodyReader(bytes.NewReader(data))
				return response, nil
			}

			_, writeErr := gzipWriter.Write(data)
			if nil != writeErr {
				_ = gzipWriter.Close()
				response.SetBodyReader(bytes.NewReader(data))
				return response, nil
			}

			closeErr := gzipWriter.Close()
			if nil != closeErr {
				response.SetBodyReader(bytes.NewReader(data))
				return response, nil
			}

			response.SetBodyReader(bytes.NewReader(buffer.Bytes()))
			response.Headers().Set("Content-Encoding", "gzip")
			response.Headers().Del("Content-Length")

			return response, nil
		}
	}
}

func DefaultCompressionMiddleware() httpcontract.Middleware {
	return CompressionMiddleware(DefaultCompressionConfig())
}
