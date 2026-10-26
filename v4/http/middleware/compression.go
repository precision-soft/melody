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

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/http"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/internal"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

/* maxConsecutiveEmptyPeekReads bounds the (0, nil) reads the peek loop accepts before declaring the reader stuck, the bound bufio uses. */
const maxConsecutiveEmptyPeekReads = 100

/* peekChunkSize is the peek buffer's starting length; the buffer doubles toward MinSize as bytes arrive. */
const peekChunkSize = 32 * 1024

/* CompressionConfig is what the compression middleware declares; the zero value is the default: gzip's default level, a 1 KiB threshold, the media and archive content types excluded, no path excluded. CompressionMiddleware copies it, lists included. */
type CompressionConfig struct {
    /* Level is gzip's level, -2 (HuffmanOnly) to 9 (BestCompression); 0 and -1 read as the default level, since 0 would label a stored body gzip and grow it; any other value is refused. */
    Level int

    /* MinSize is the body length in bytes below which a response is sent as it is: 1024 by default, at least 1, unlimited refused. The middleware holds up to this many bytes of a body of unknown length before deciding, so a large threshold is memory per response. */
    MinSize bound.Count

    /* ExcludedContentTypes are the content type prefixes never compressed; nil is the default family, image/, video/, audio/, application/zip, application/gzip and application/x-gzip, and an empty list excludes none. */
    ExcludedContentTypes []string

    /* ExcludedPaths are the request path prefixes never compressed; nil or empty excludes none. */
    ExcludedPaths []string
}

var compressionMinSizeRule = bound.Rule[int]{
    Name:    "CompressionConfig.MinSize",
    Default: 1024,
    Minimum: 1,
}

type compressionSettings struct {
    level                int
    minSize              int
    excludedContentTypes []string
    excludedPaths        []string
}

func defaultExcludedContentTypes() []string {
    return []string{
        "image/",
        "video/",
        "audio/",
        "application/zip",
        "application/gzip",
        "application/x-gzip",
    }
}

func resolveCompressionConfig(config CompressionConfig) compressionSettings {
    level := config.Level
    /* level 0 is gzip's NoCompression, which would label a stored body gzip and grow it by the framing */
    if gzip.NoCompression == level {
        level = gzip.DefaultCompression
    }

    if gzip.HuffmanOnly > level || gzip.BestCompression < level {
        exception.Panic(
            exception.NewError(
                "the compression level is outside gzip's range",
                exceptioncontract.Context{
                    "field":   "CompressionConfig.Level",
                    "value":   strconv.Itoa(config.Level),
                    "minimum": strconv.Itoa(gzip.HuffmanOnly),
                    "maximum": strconv.Itoa(gzip.BestCompression),
                },
                nil,
            ),
        )
    }

    minSize, minSizeErr := bound.Resolve(config.MinSize, compressionMinSizeRule)
    if nil != minSizeErr {
        exception.Panic(exception.FromError(minSizeErr))
    }

    excludedContentTypes := defaultExcludedContentTypes()
    if nil != config.ExcludedContentTypes {
        excludedContentTypes = append([]string{}, config.ExcludedContentTypes...)
    }

    return compressionSettings{
        level:                level,
        minSize:              minSize.Value,
        excludedContentTypes: excludedContentTypes,
        excludedPaths:        append([]string{}, config.ExcludedPaths...),
    }
}

/* CompressionMiddleware gzips a response the client accepts, past the threshold and outside the exclusions; the configuration is resolved when the middleware is built. */
func CompressionMiddleware(config CompressionConfig) httpcontract.Middleware {
    settings := resolveCompressionConfig(config)

    return func(next httpcontract.Handler) httpcontract.Handler {
        return func(runtimeInstance runtimecontract.Runtime, writer nethttp.ResponseWriter, request httpcontract.Request) (httpcontract.Response, error) {
            response, nextMiddlewareErr := next(runtimeInstance, writer, request)
            if nil != nextMiddlewareErr {
                return response, nextMiddlewareErr
            }

            /* read through, not compared: a handler answering a typed nil is handed on as nil, so the kernel answers its empty 204 rather than this middleware dereferencing it */
            if true == internal.IsNilInterface(response) {
                return nil, nil
            }

            if nil == response.Headers() {
                response.SetHeaders(make(nethttp.Header))
            }

            /* emitted on every path, the skipped ones included, so a shared cache cannot serve one encoding to a client that asked for another */
            addVaryAcceptEncoding(response.Headers())

            httpRequest := request.HttpRequest()
            if nil == httpRequest {
                return response, nil
            }

            /* the excluded prefixes are read against the spelling the router matched */
            requestPath := http.RequestPathAsRouted(internal.RequestPathAsSent(httpRequest.URL))

            for _, excludedPath := range settings.excludedPaths {
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
            for _, excludedContentType := range settings.excludedContentTypes {
                if "" != excludedContentType && true == strings.HasPrefix(contentType, excludedContentType) {
                    return response, nil
                }
            }

            /* every line of a repeated Accept-Encoding field is joined before parsing, as the Accept readers do */
            if false == acceptsGzip(strings.Join(httpRequest.Header.Values("Accept-Encoding"), ",")) {
                return response, nil
            }

            contentLengthString := response.Headers().Get("Content-Length")
            if "" != contentLengthString {
                value, parseErr := strconv.Atoi(contentLengthString)
                if nil == parseErr && settings.minSize > value {
                    return response, nil
                }
            }

            originalReader := response.BodyReader()

            /* the peek buffer grows with the bytes read, since MinSize has no upper bound */
            peekSize := settings.minSize
            /* an untyped body is sniffed below from the bytes net/http would read for an identity body, its first 512 */
            if "" == contentType && sniffLength > peekSize {
                peekSize = sniffLength
            }
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
                    /* a (0, nil) read on a non-empty slice must be tolerated, not looped on forever, so the loop gives up as bufio does */
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
                closeBodyReader(originalReader)
                return response, peekErr
            }

            if peeked < settings.minSize {
                closeBodyReader(originalReader)
                response.SetBodyReader(bytes.NewReader(peekBuffer[:peeked]))
                return response, nil
            }

            source := io.MultiReader(bytes.NewReader(peekBuffer[:peeked]), originalReader)

            /* net/http sniffs the type of an identity body that carries none but never of an encoded one, so the type is sniffed here from the peeked bytes, and the two representations carry the same type; a sniffed type the configuration excludes goes out uncompressed */
            if "" == contentType {
                contentType = nethttp.DetectContentType(peekBuffer[:peeked])
                response.Headers().Set("Content-Type", contentType)

                for _, excludedContentType := range settings.excludedContentTypes {
                    if "" != excludedContentType && true == strings.HasPrefix(contentType, excludedContentType) {
                        response.SetBodyReader(&peekedBodyReader{reader: source, original: originalReader})
                        return response, nil
                    }
                }
            }

            pipeReader, pipeWriter := io.Pipe()
            compressionDone := make(chan struct{})
            /* closed once, by whichever of the two goroutines gets there first: a client that leaves stops the source being read, since gzip consumes megabytes of a compressible body before it writes a byte the closed pipe could refuse */
            sourceCloser := &onceClosingReader{reader: originalReader}
            go streamGzipCompressInto(pipeWriter, source, sourceCloser, settings.level, compressionDone)
            /* the pipe reader is tied to the request lifecycle, so an outer middleware panicking after next() cannot leave the gzip goroutine blocked in pipe.Write */
            go closePipeReaderOnRequestUnwind(httpRequest.Context(), pipeReader, compressionDone, sourceCloser)

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

    /* a header the member cap cut reads as unparsable, since a member past the cap may carry the refusal gzip;q=0 */
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

        /* a q outside the RFC 7231 qvalue grammar drops the entry, as every negotiating reader in this tree does */
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

        /* a repeated coding resolves to its higher q, the tie rule of every Accept reader in this tree */
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

/* closeBodyReader closes the application's body reader on the request goroutine, where a panic in its Close reaches the kernel's recovery as any handler panic does; its error is dropped, the body being abandoned */
func closeBodyReader(reader io.Reader) {
    closer, ok := reader.(io.Closer)
    if false == ok {
        return
    }

    _ = closer.Close()
}

/* closeBodyReaderQuiet closes the application's body reader on the gzip goroutine and on the unwind watcher, where a panic has no handler but the process; it is contained here, quietly, as its error is */
func closeBodyReaderQuiet(reader io.Reader) {
    defer func() {
        _ = recover()
    }()

    closeBodyReader(reader)
}

/* closePipeReaderOnRequestUnwind closes the gzip pipe reader and the source when the request context is cancelled, and returns without touching it once compression finishes normally. */
func closePipeReaderOnRequestUnwind(requestContext context.Context, pipeReader *io.PipeReader, compressionDone <-chan struct{}, sourceCloser io.Closer) {
    select {
    case <-compressionDone:
        return
    case <-requestContext.Done():
        _ = pipeReader.CloseWithError(requestContext.Err())
        _ = sourceCloser.Close()
    }
}

/* sniffLength is how many leading bytes net/http reads to sniff the type of a body that carries none */
const sniffLength = 512

/* peekedBodyReader replays the peeked bytes ahead of the rest of the body and closes the application's reader, so a body handed back after the peek is closed as it would have been */
type peekedBodyReader struct {
    reader   io.Reader
    original io.Reader
}

func (instance *peekedBodyReader) Read(buffer []byte) (int, error) {
    return instance.reader.Read(buffer)
}

func (instance *peekedBodyReader) Close() error {
    closer, ok := instance.original.(io.Closer)
    if false == ok {
        return nil
    }

    return closer.Close()
}

/* onceClosingReader closes the reader it carries once, whoever asks first; a reader that is not an io.Closer is left alone */
type onceClosingReader struct {
    reader io.Reader
    once   sync.Once
}

func (instance *onceClosingReader) Read(buffer []byte) (int, error) {
    return instance.reader.Read(buffer)
}

func (instance *onceClosingReader) Close() error {
    instance.once.Do(func() {
        closeBodyReaderQuiet(instance.reader)
    })

    return nil
}

/* the pools are indexed by compression level, since a writer keeps the level it was built with; an invalid level fails at creation. */
const lowestGzipLevel = gzip.HuffmanOnly
const highestGzipLevel = gzip.BestCompression

var gzipWriterPools [highestGzipLevel - lowestGzipLevel + 1]sync.Pool

func gzipWriterPoolFor(level int) *sync.Pool {
    if lowestGzipLevel > level || highestGzipLevel < level {
        return nil
    }

    return &gzipWriterPools[level-lowestGzipLevel]
}

/* acquireGzipWriter hands out a pooled writer, reset onto this response's pipe, since a fresh one allocates its whole deflate state. */
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

/* releaseGzipWriter is called only once Close has returned; Reset clears a failed Close's error, so the writer stays reusable. */
func releaseGzipWriter(gzipWriter *gzip.Writer, level int) {
    pool := gzipWriterPoolFor(level)
    if nil == pool {
        return
    }

    pool.Put(gzipWriter)
}

/* copyIntoGzipWriterSafely contains a panic of the application's body reader, since the copy runs on a goroutine no recovery stands over. The panic returns as the copy error, which fails this response alone. */
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

    /* reached only after gzipWriter.Close returned, the condition the pool depends on */
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
