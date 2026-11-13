package http

import (
    "fmt"
    "io"
    nethttp "net/http"
    "reflect"
    "strconv"
    "strings"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
)

type ServerSentEvent struct {
    Id    string
    Event string
    Data  string
    Retry int
}

/* NewServerSentEventWriter refuses before the commit, since after it nothing can be answered; it must not outlive the handler */
func NewServerSentEventWriter(writer nethttp.ResponseWriter) (*ServerSentEventWriter, error) {
    if nil == writer {
        return nil, exception.NewError("response writer may not be nil", nil, nil)
    }

    if commitRecorder, isCommitRecorder := writer.(headerCommitRecorder); true == isCommitRecorder {
        if true == commitRecorder.HeadersWritten() {
            return nil, exception.NewError("response is already committed", nil, nil)
        }
    }

    flusher, isFlusher, chainErr := streamingFlusherOf(writer)
    if nil != chainErr {
        return nil, chainErr
    }

    if false == isFlusher {
        return nil, exception.NewError("response writer does not support streaming", nil, nil)
    }

    header := writer.Header()
    header.Set("Content-Type", "text/event-stream")
    header.Set("Cache-Control", "no-cache")
    header.Set("Connection", "keep-alive")
    header.Set("X-Accel-Buffering", "no")

    writer.WriteHeader(nethttp.StatusOK)
    flusher.Flush()

    return &ServerSentEventWriter{
        writer:  writer,
        flusher: flusher,
    }, nil
}

const responseWriterChainLimit = 65536

/* the kernel's recording writer may flush nothing; the ring is refused here since http.ResponseController walks the chain unbounded */
func streamingFlusherOf(writer nethttp.ResponseWriter) (nethttp.Flusher, bool, error) {
    current := writer
    seen := map[nethttp.ResponseWriter]struct{}{}

    for link := 0; ; link++ {
        if responseWriterChainLimit <= link {
            return nil, false, exception.NewError(
                "response writer chain is longer than the limit",
                map[string]any{"writerType": fmt.Sprintf("%T", writer), "limit": responseWriterChainLimit},
                nil,
            )
        }

        if reflect.Pointer == reflect.ValueOf(current).Kind() {
            if _, isSeen := seen[current]; true == isSeen {
                return nil, false, exception.NewError(
                    "response writer chain reaches itself",
                    map[string]any{"writerType": fmt.Sprintf("%T", writer), "repeatedType": fmt.Sprintf("%T", current)},
                    nil,
                )
            }

            seen[current] = struct{}{}
        }

        unwrapper, isUnwrapper := current.(interface{ Unwrap() nethttp.ResponseWriter })
        if false == isUnwrapper {
            break
        }

        unwrapped := unwrapper.Unwrap()
        if nil == unwrapped {
            break
        }

        current = unwrapped
    }

    if _, isFlusher := current.(nethttp.Flusher); false == isFlusher {
        return nil, false, nil
    }

    /* the flush goes through the outermost writer, so every wrapper records the commit */
    flusher, isFlusher := writer.(nethttp.Flusher)
    if false == isFlusher {
        return nil, false, nil
    }

    return flusher, true, nil
}

type ServerSentEventWriter struct {
    mutex               sync.Mutex
    writer              nethttp.ResponseWriter
    flusher             nethttp.Flusher
    broken              bool
    writeBudget         time.Duration
    clock               serverSentEventClock
    deadlineUnsupported bool
}

/* serverSentEventClock is an interface rather than a func value, so the writer stays comparable */
type serverSentEventClock interface {
    Now() time.Time
}

type systemServerSentEventClock struct{}

func (instance systemServerSentEventClock) Now() time.Time {
    return time.Now()
}

/* WithWriteBudget re-arms the write deadline to budget on every frame; zero leaves the deadline the server armed */
func (instance *ServerSentEventWriter) WithWriteBudget(budget time.Duration) *ServerSentEventWriter {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if 0 > budget {
        budget = 0
    }

    instance.writeBudget = budget

    return instance
}

/* Send refuses an event name with no data, a frame with no field and, after a frame fails part way, every later call */
func (instance *ServerSentEventWriter) Send(event ServerSentEvent) error {
    if refusalErr := validateServerSentEvent(event); nil != refusalErr {
        return refusalErr
    }

    var builder strings.Builder

    if "" != event.Id {
        builder.WriteString("id: ")
        builder.WriteString(sanitizeServerSentEventId(event.Id))
        builder.WriteString(serverSentEventLineTerminator)
    }

    if "" != event.Event {
        builder.WriteString("event: ")
        builder.WriteString(sanitizeServerSentEventField(event.Event))
        builder.WriteString(serverSentEventLineTerminator)
    }

    if 0 < event.Retry {
        builder.WriteString("retry: ")
        builder.WriteString(strconv.Itoa(event.Retry))
        builder.WriteString(serverSentEventLineTerminator)
    }

    if "" != event.Data {
        normalizedData := serverSentEventLineEndingReplacer.Replace(event.Data)
        for _, line := range strings.Split(normalizedData, "\n") {
            builder.WriteString("data: ")
            builder.WriteString(line)
            builder.WriteString(serverSentEventLineTerminator)
        }
    }

    builder.WriteString(serverSentEventLineTerminator)

    return instance.writeFrame(builder.String())
}

func validateServerSentEvent(event ServerSentEvent) error {
    if 0 > event.Retry {
        return exception.NewError(
            "server sent event retry may not be negative",
            map[string]any{
                "retry": event.Retry,
            },
            nil,
        )
    }

    if "" != event.Id && "" == sanitizeServerSentEventId(event.Id) {
        return exception.NewError("server sent event id is empty once its control bytes are removed", nil, nil)
    }

    if "" != event.Event && "" == sanitizeServerSentEventField(event.Event) {
        return exception.NewError("server sent event name is empty once its control bytes are removed", nil, nil)
    }

    /* dispatch never fires a named listener with no data; an id or a retry with no data takes effect and is allowed */
    if "" == event.Data && "" != event.Event {
        return exception.NewError(
            "server sent event carries an event name and no data, so it would dispatch nothing",
            map[string]any{
                "event": event.Event,
            },
            nil,
        )
    }

    if "" == event.Data && "" == event.Id && 0 == event.Retry {
        return exception.NewError("server sent event is empty", nil, nil)
    }

    return nil
}

func (instance *ServerSentEventWriter) writeFrame(frame string) error {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if nil == instance.writer || nil == instance.flusher {
        return exception.NewError("server sent event writer was not constructed through NewServerSentEventWriter", nil, nil)
    }

    if true == instance.broken {
        return exception.NewError("server sent event stream is broken by an earlier partial write", nil, nil)
    }

    instance.rearmWriteDeadlineLocked()

    written, writeErr := io.WriteString(instance.writer, frame)
    if nil != writeErr {
        if 0 < written {
            instance.broken = true
        }

        return writeErr
    }

    instance.flusher.Flush()

    return nil
}

func (instance *ServerSentEventWriter) rearmWriteDeadlineLocked() {
    if 0 >= instance.writeBudget || true == instance.deadlineUnsupported {
        return
    }

    var clock serverSentEventClock = systemServerSentEventClock{}
    if nil != instance.clock {
        clock = instance.clock
    }

    deadlineErr := nethttp.NewResponseController(instance.writer).SetWriteDeadline(clock.Now().Add(instance.writeBudget))
    if true == exception.ChainIs(deadlineErr, nethttp.ErrNotSupported) {
        instance.deadlineUnsupported = true
    }
}

const (
    serverSentEventLineTerminator  = "\n"
    serverSentEventFrameTerminator = "\n\n"
)

var (
    serverSentEventControlByteReplacer = strings.NewReplacer("\r", "", "\n", "", "\x00", "")
    serverSentEventLineEndingReplacer  = strings.NewReplacer("\r\n", "\n", "\r", "\n")
)

func sanitizeServerSentEventField(value string) string {
    return serverSentEventControlByteReplacer.Replace(value)
}

func sanitizeServerSentEventId(value string) string {
    return serverSentEventControlByteReplacer.Replace(value)
}

/* Comment writes one comment frame ended by a blank line, so a keepalive is observable frame by frame */
func (instance *ServerSentEventWriter) Comment(text string) error {
    return instance.writeFrame(": " + sanitizeServerSentEventField(text) + serverSentEventFrameTerminator)
}

func (instance *ServerSentEventWriter) Ping() error {
    return instance.Comment("")
}
