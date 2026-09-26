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

    "github.com/precision-soft/melody/v2/exception"
    "github.com/precision-soft/melody/v2/http"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    "github.com/precision-soft/melody/v2/internal"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
)

/* maxConsecutiveEmptyPeekReads bounds how many (0, nil) results the peek loop accepts from a response body before it declares the reader stuck. The value matches the identical bound in bufio, so a reader that already works under bufio.Reader keeps working here. */
const maxConsecutiveEmptyPeekReads = 100

/* peekChunkSize is the peek buffer's starting length; the buffer doubles from here toward MinSize as bytes actually arrive. */
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
    /* nil reads as the default configuration, as the cors middleware and the route group read theirs. A non-nil configuration is copied before normalization, so the caller's object is not rewritten. */
    if nil == config {
        config = DefaultCompressionConfig()
    } else {
        config = NewCompressionConfig(config.level, config.minSize, config.excludedContentTypes, config.excludedPaths)
    }

    if gzip.HuffmanOnly > config.Level() || gzip.BestCompression < config.Level() {
        config.SetLevel(gzip.DefaultCompression)
    }

    /* a non-positive minimum is not a threshold at all — zero would compress every response and a negative one would make the peek loop's arithmetic lie — so the whole range normalizes to the default */
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

            /* emitted on every path so a shared cache cannot serve one encoding to a client that asked for another: the paths that skip compression are negotiated against Accept-Encoding too, and a public response stored under the URL alone would be replayed to a client that cannot decode it. */
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

            /* every line of a repeated Accept-Encoding field is joined before parsing, as the Accept readers join theirs: the header is list-typed, so a coding named on a second line counts */
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

            /* the peek buffer grows with the bytes actually read: MinSize has no upper bound, so allocating the full threshold upfront would make every eligible response allocate that size before a byte arrives. */
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

    /* a header the member cap cut is read as unparsable, so nothing is compressed: the members past the cap can carry a refusal (gzip;q=0) the members before it do not */
    entries, cut := internal.SplitOutsideQuotes(acceptEncoding, ',')
    if true == cut {
        return false
    }

    for _, rawEntry := range entries {
        entry := strings.TrimSpace(rawEntry)
        if "" == entry {
            continue
        }

        parts, cut := internal.SplitOutsideQuotes(entry, ';')
        if true == cut {
            return false
        }

        codingName := strings.ToLower(strings.TrimSpace(parts[0]))
        if "" == codingName {
            continue
        }

        /* an entry whose q falls outside the RFC 7231 qvalue grammar is dropped whole, the rule every negotiating reader in this tree applies, so q=Inf and q=NaN switch nothing */
        quality := 1.0
        qualityValid := true
        for _, rawParam := range parts[1:] {
            /* the parameter name is case-insensitive, so a refusal spelled "Q=0" weighs the same as "q=0" */
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

        /* a repeated coding resolves to its higher q, the tie rule of every Accept reader in this tree, so gzip;q=0.5, gzip;q=0 answers the same in either order */
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

/* closePipeReaderOnRequestUnwind closes the gzip pipe reader when the request context is cancelled, so a compression goroutine whose response was abandoned by a panicking outer middleware cannot block forever in pipe.Write; it returns without touching the pipe once compression finishes normally, so the successful path does not disturb the served body */
func closePipeReaderOnRequestUnwind(requestContext context.Context, pipeReader *io.PipeReader, compressionDone <-chan struct{}) {
    select {
    case <-compressionDone:
        return
    case <-requestContext.Done():
        _ = pipeReader.CloseWithError(requestContext.Err())
    }
}

/* the pools are indexed by compression level, because a writer carries the level it was built with and resetting it does not change it. gzip accepts HuffmanOnly through BestCompression and refuses anything else, so a level outside that range never reaches a pool: it fails at creation, which is where an invalid configuration should be reported. */
const lowestGzipLevel = gzip.HuffmanOnly
const highestGzipLevel = gzip.BestCompression

var gzipWriterPools [highestGzipLevel - lowestGzipLevel + 1]sync.Pool

func gzipWriterPoolFor(level int) *sync.Pool {
    if lowestGzipLevel > level || highestGzipLevel < level {
        return nil
    }

    return &gzipWriterPools[level-lowestGzipLevel]
}

/* acquireGzipWriter hands out a writer whose deflate state is already allocated: a fresh one costs about 800 KiB of window and hash tables per compressed response. The writer is reset onto this response's pipe before it is handed over, which is what keeps two responses apart. */
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

/* releaseGzipWriter is called only once Close has returned. A writer still inside a response holds the deflate state of a body that has not been terminated yet, and handing it to another response would interleave the two into one stream; Reset clears the error a failed Close left behind, so a writer whose response ended badly is still reusable. */
func releaseGzipWriter(gzipWriter *gzip.Writer, level int) {
    pool := gzipWriterPoolFor(level)
    if nil == pool {
        return
    }

    pool.Put(gzipWriter)
}

/* copyIntoGzipWriterSafely contains a panic raised by the application's body reader while it is compressed: the copy runs on a goroutine this middleware started, where no recovery of the kernel or of net/http stands, so a panic there would take the process down. It travels back as the copy error the caller reports through the pipe, and this one response fails. */
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

    /* every path below reaches this only after gzipWriter.Close has returned, which is the condition the pool depends on */
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
