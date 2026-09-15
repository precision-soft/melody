package middleware

import (
    "bytes"
    "compress/gzip"
    "context"
    "io"
    nethttp "net/http"
    "strconv"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const maxConsecutiveEmptyPeekReads = 100

const peekChunkSize = 32 * 1024

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

    if nil == config {
        config = DefaultCompressionConfig()
    } else {
        config = NewCompressionConfig(config.level, config.minSize, config.excludedContentTypes, config.excludedPaths)
    }

    if gzip.HuffmanOnly > config.Level() || gzip.BestCompression < config.Level() {
        config.SetLevel(gzip.DefaultCompression)
    }

    if 0 >= config.MinSize() {
        config.SetMinSize(1024)
    }

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response, nextMiddlewareErr := next(runtimeInstance, writer, request)
            if nil != nextMiddlewareErr || nil == response {
                return response, nextMiddlewareErr
            }

            if nil == response.Headers() {
                response.SetHeaders(make(nethttp.Header))
            }

            addVaryAcceptEncoding(response.Headers())

            httpRequest := request.HttpRequest()
            if nil == httpRequest {
                return response, nil
            }

            requestPath := http.RequestPathAsRouted(httpRequest.URL.EscapedPath())

            for _, excludedPath := range config.ExcludedPaths() {
                if "" != excludedPath && true == strings.HasPrefix(requestPath, excludedPath) {
                    return response, nil
                }
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

            if false == acceptsGzip(strings.Join(httpRequest.Header.Values("Accept-Encoding"), ",")) {
                return response, nil
            }

            contentLengthString := response.Headers().Get("Content-Length")
            if "" != contentLengthString {
                value, parseErr := strconv.Atoi(contentLengthString)
                if nil == parseErr && config.MinSize() > value {
                    return response, nil
                }
            }

            originalReader := response.BodyReader()

            peekSize := config.MinSize()
            initialLength := peekSize
            if peekChunkSize < initialLength {
                initialLength = peekChunkSize
            }
            peekBuffer := make([]byte, initialLength)
            peeked := 0
            emptyReads := 0
            var peekErr error
            for peeked < peekSize {
                if peeked == len(peekBuffer) {
                    nextLength := len(peekBuffer) * 2
                    if peekSize < nextLength {
                        nextLength = peekSize
                    }

                    grown := make([]byte, nextLength)
                    copy(grown, peekBuffer)
                    peekBuffer = grown
                }

                readCount, readErr := originalReader.Read(peekBuffer[peeked:])
                peeked += readCount
                if nil != readErr {
                    peekErr = readErr
                    break
                }

                if 0 == readCount {

                    emptyReads++
                    if maxConsecutiveEmptyPeekReads <= emptyReads {
                        peekErr = io.ErrNoProgress
                        break
                    }

                    continue
                }

                emptyReads = 0
            }

            if nil != peekErr && io.EOF != peekErr {
                closeBodyReaderQuiet(originalReader)
                return response, peekErr
            }

            if peeked < peekSize {
                closeBodyReaderQuiet(originalReader)
                response.SetBodyReader(bytes.NewReader(peekBuffer[:peeked]))
                return response, nil
            }

            source := io.MultiReader(bytes.NewReader(peekBuffer[:peeked]), originalReader)

            pipeReader, pipeWriter := io.Pipe()
            compressionDone := make(chan struct{})
            go streamGzipCompressInto(pipeWriter, source, originalReader, config.Level(), compressionDone)

            go closePipeReaderOnRequestUnwind(httpRequest.Context(), pipeReader, compressionDone)

            response.SetBodyReader(pipeReader)
            response.Headers().Set("Content-Encoding", "gzip")
            response.Headers().Del("Content-Length")

            return response, nil
        }
    }
}

func acceptsGzip(acceptEncoding string) bool {
    if "" == acceptEncoding {
        return false
    }

    gzipQuality := -1.0
    starQuality := -1.0

    for _, rawEntry := range internal.SplitOutsideQuotes(acceptEncoding, ',') {
        entry := strings.TrimSpace(rawEntry)
        if "" == entry {
            continue
        }

        parts := internal.SplitOutsideQuotes(entry, ';')
        codingName := strings.ToLower(strings.TrimSpace(parts[0]))
        if "" == codingName {
            continue
        }

        quality := 1.0
        qualityValid := true
        for _, rawParam := range parts[1:] {

            param := strings.ToLower(strings.TrimSpace(rawParam))
            if false == strings.HasPrefix(param, "q=") {
                continue
            }

            parsedQuality, valid := internal.ParseQualityValue(strings.TrimSpace(param[2:]))
            if false == valid {
                qualityValid = false

                continue
            }

            quality = parsedQuality
        }

        if false == qualityValid {
            continue
        }

        if "gzip" == codingName && quality > gzipQuality {
            gzipQuality = quality
        } else if "*" == codingName && quality > starQuality {
            starQuality = quality
        }
    }

    if 0 <= gzipQuality {
        return 0 < gzipQuality
    }

    if 0 <= starQuality {
        return 0 < starQuality
    }

    return false
}

func addVaryAcceptEncoding(headers nethttp.Header) {
    for _, existing := range headers.Values("Vary") {
        for _, token := range strings.Split(existing, ",") {
            if "accept-encoding" == strings.ToLower(strings.TrimSpace(token)) {
                return
            }
        }
    }

    headers.Add("Vary", "Accept-Encoding")
}

func closeBodyReaderQuiet(reader io.Reader) {
    closer, ok := reader.(io.Closer)
    if false == ok {
        return
    }

    _ = closer.Close()
}

func closePipeReaderOnRequestUnwind(requestContext context.Context, pipeReader *io.PipeReader, compressionDone <-chan struct{}) {
    select {
    case <-compressionDone:
        return
    case <-requestContext.Done():
        _ = pipeReader.CloseWithError(requestContext.Err())
    }
}

const lowestGzipLevel = gzip.HuffmanOnly
const highestGzipLevel = gzip.BestCompression

var gzipWriterPools [highestGzipLevel - lowestGzipLevel + 1]sync.Pool

func gzipWriterPoolFor(level int) *sync.Pool {
    if lowestGzipLevel > level || highestGzipLevel < level {
        return nil
    }

    return &gzipWriterPools[level-lowestGzipLevel]
}

func acquireGzipWriter(destination io.Writer, level int) (*gzip.Writer, error) {
    pool := gzipWriterPoolFor(level)
    if nil == pool {
        return gzip.NewWriterLevel(destination, level)
    }

    pooled, isWriter := pool.Get().(*gzip.Writer)
    if false == isWriter {
        return gzip.NewWriterLevel(destination, level)
    }

    pooled.Reset(destination)

    return pooled, nil
}

func releaseGzipWriter(gzipWriter *gzip.Writer, level int) {
    pool := gzipWriterPoolFor(level)
    if nil == pool {
        return
    }

    pool.Put(gzipWriter)
}

func copyIntoGzipWriterSafely(gzipWriter *gzip.Writer, source io.Reader) (copyErr error) {
    defer func() {
        recoveredValue := recover()
        if nil == recoveredValue {
            return
        }

        copyErr = exception.NewError(
            "the response body reader panicked while the response was being compressed",
            nil,
            http.RecoverToError(recoveredValue),
        )
    }()

    _, copyErr = io.Copy(gzipWriter, source)

    return copyErr
}

func streamGzipCompressInto(pipeWriter *io.PipeWriter, source io.Reader, sourceCloser io.Reader, level int, compressionDone chan<- struct{}) {
    defer close(compressionDone)
    defer closeBodyReaderQuiet(sourceCloser)

    gzipWriter, gzipErr := acquireGzipWriter(pipeWriter, level)
    if nil != gzipErr {
        _ = pipeWriter.CloseWithError(
            exception.NewError("failed to initialize gzip writer", nil, gzipErr),
        )
        return
    }

    defer releaseGzipWriter(gzipWriter, level)

    copyErr := copyIntoGzipWriterSafely(gzipWriter, source)
    if nil != copyErr {
        _ = gzipWriter.Close()
        _ = pipeWriter.CloseWithError(copyErr)
        return
    }

    closeErr := gzipWriter.Close()
    if nil != closeErr {
        _ = pipeWriter.CloseWithError(closeErr)
        return
    }

    _ = pipeWriter.Close()
}

func DefaultCompressionMiddleware() httpcontract.Middleware {
    return CompressionMiddleware(DefaultCompressionConfig())
}
