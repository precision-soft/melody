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
    /* nil reads as the default configuration, the way the cors middleware and the route group read their absent options — dereferencing it below answered a wiring shorthand with a raw panic where every sibling door answers the defaults. The non-nil configuration is copied before the normalization lands, so the caller's own object is not rewritten by construction — the static file server copies its options at construction for the same reason. */
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

            /* every line of a repeated Accept-Encoding field is joined before parsing, the way the Accept readers join theirs: the header is list-typed, and reading only the first line dropped a coding the client named on the second */
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

            /* the peek buffer grows with the bytes actually read instead of being allocated at the full threshold upfront: MinSize carries no upper bound, so an oversized — or unit-confused — minimum turned every eligible response into an allocation of that size before a single byte arrived, when a response below the threshold needs no more memory than its own length. */
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

        /* an entry whose q parameter falls outside the RFC 7231 qvalue grammar is dropped whole, the rule every negotiating reader in this tree applies: a bare float parse let q=Inf switch the compression on and q=NaN switch it off, weights no grammar-conforming client can send */
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

        /* a repeated coding resolves to its higher q, the tie rule of every Accept reader in this tree: last-wins made gzip;q=0.5, gzip;q=0 and its reversal answer differently for one statement */
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

/* acquireGzipWriter hands out a writer whose deflate state is already allocated. A fresh one costs about 800 KiB of window and hash tables, and the middleware built one per compressed response: at a thousand responses a second that is the better part of a gigabyte of garbage a second, on the hot path of every request, and the allocation dominated the compression itself. The writer is reset onto this response's pipe before it is handed over — that reset is what separates the two responses, and without it the second body would be written into the first one's pipe. */
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
