package middleware

import (
    "bytes"
    "compress/gzip"
    "context"
    "io"
    nethttp "net/http"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/exception"
    httpcontract "github.com/precision-soft/melody/http/contract"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

/* maxConsecutiveEmptyPeekReads bounds how many (0, nil) results the peek loop accepts from a response body before it declares the reader stuck. The value matches the identical bound in bufio, so a reader that already works under bufio.Reader keeps working here. */
const maxConsecutiveEmptyPeekReads = 100

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
    if gzip.HuffmanOnly > config.Level() || gzip.BestCompression < config.Level() {
        config.SetLevel(gzip.DefaultCompression)
    }

    /* a negative minimum would reach make([]byte, peekSize) below and panic on every request, so normalize the whole non-positive range, not just zero */
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

            /* emitted on every path so a shared cache cannot serve one encoding of the URL to a client that asked for another. The paths that skip compression need it most: a body some other layer already encoded, an excluded path and an excluded content type are all negotiated against Accept-Encoding just the same, and a Cache-Control: public response stored under the URL alone would then be replayed to a client that cannot decode it. */
            addVaryAcceptEncoding(response.Headers())

            httpRequest := request.HttpRequest()
            if nil == httpRequest {
                return response, nil
            }

            for _, excludedPath := range config.ExcludedPaths() {
                if "" != excludedPath && true == strings.HasPrefix(httpRequest.URL.Path, excludedPath) {
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

            if false == acceptsGzip(httpRequest.Header.Get("Accept-Encoding")) {
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
            peekBuffer := make([]byte, peekSize)
            peeked := 0
            emptyReads := 0
            var peekErr error
            for peeked < peekSize {
                readCount, readErr := originalReader.Read(peekBuffer[peeked:])
                peeked += readCount
                if nil != readErr {
                    peekErr = readErr
                    break
                }

                if 0 == readCount {
                    /* the destination slice is never empty here, so io.Reader permits (0, nil) only as a state the caller must tolerate rather than loop on; an unbounded loop would pin this request's goroutine at full processor for the lifetime of the process. Give up exactly as bufio does and let the request fail instead. */
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
            /* if an outer middleware panics after next() returned, the kernel drops this response without closing its body, so the gzip goroutine would block forever in pipe.Write and pin the original reader's descriptor; tie the pipe reader to the request lifecycle so it is closed when the request unwinds */
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

    for _, rawEntry := range strings.Split(acceptEncoding, ",") {
        entry := strings.TrimSpace(rawEntry)
        if "" == entry {
            continue
        }

        parts := strings.Split(entry, ";")
        codingName := strings.ToLower(strings.TrimSpace(parts[0]))
        if "" == codingName {
            continue
        }

        quality := 1.0
        for _, rawParam := range parts[1:] {
            param := strings.TrimSpace(rawParam)
            if false == strings.HasPrefix(param, "q=") {
                continue
            }

            parsedQuality, parseErr := strconv.ParseFloat(strings.TrimSpace(param[2:]), 64)
            if nil == parseErr {
                quality = parsedQuality
            }
        }

        if "gzip" == codingName {
            gzipQuality = quality
        } else if "*" == codingName {
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

/* closePipeReaderOnRequestUnwind closes the gzip pipe reader when the request context is cancelled, so a compression goroutine whose response was abandoned by a panicking outer middleware cannot block forever in pipe.Write; it returns without touching the pipe once compression finishes normally, so the successful path does not disturb the served body */
func closePipeReaderOnRequestUnwind(requestContext context.Context, pipeReader *io.PipeReader, compressionDone <-chan struct{}) {
    select {
    case <-compressionDone:
        return
    case <-requestContext.Done():
        _ = pipeReader.CloseWithError(requestContext.Err())
    }
}

func streamGzipCompressInto(pipeWriter *io.PipeWriter, source io.Reader, sourceCloser io.Reader, level int, compressionDone chan<- struct{}) {
    defer close(compressionDone)
    defer closeBodyReaderQuiet(sourceCloser)

    gzipWriter, gzipErr := gzip.NewWriterLevel(pipeWriter, level)
    if nil != gzipErr {
        _ = pipeWriter.CloseWithError(
            exception.NewError("failed to initialize gzip writer", nil, gzipErr),
        )
        return
    }

    _, copyErr := io.Copy(gzipWriter, source)
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
