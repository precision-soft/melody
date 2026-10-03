package http

import (
    "errors"
    "io"
    nethttp "net/http"
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

/* NewServerSentEventWriter commits the event-stream headers and returns the writer that emits frames onto them. It refuses a response already committed and a connection that cannot flush, read through the wrappers to the connection, since after the commit nothing can be answered. The writer is safe for concurrent use and must not outlive the handler. */
func NewServerSentEventWriter(writer nethttp.ResponseWriter) (*ServerSentEventWriter, error) {
    if nil == writer {
        return nil, exception.NewError("response writer may not be nil", nil, nil)
    }

    if commitRecorder, isCommitRecorder := writer.(headerCommitRecorder); true == isCommitRecorder {
        if true == commitRecorder.HeadersWritten() {
            return nil, exception.NewError("response is already committed", nil, nil)
        }
    }

    flusher, isFlusher := streamingFlusherOf(writer)
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

/* streamingFlusherOf finds the flusher that reaches the connection, unwrapping the writer chain as http.ResponseController does, since the kernel's recording writer carries a Flush that may flush nothing. */
func streamingFlusherOf(writer nethttp.ResponseWriter) (nethttp.Flusher, bool) {
    current := writer

    for {
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
        return nil, false
    }

    /* the flush goes through the outermost writer, so every wrapper records the commit */
    flusher, isFlusher := writer.(nethttp.Flusher)
    if false == isFlusher {
        return nil, false
    }

    return flusher, true
}

type ServerSentEventWriter struct {
    mutex       sync.Mutex
    writer      nethttp.ResponseWriter
    flusher     nethttp.Flusher
    broken      bool
    writeBudget time.Duration
    clock       serverSentEventClock
    /* set once the writer answered ErrNotSupported to a deadline, a property of the writer, so later frames do not ask again */
    deadlineUnsupported bool
}

/* serverSentEventClock is the instant a frame's deadline is counted from; an interface rather than a func value, so the writer stays comparable. */
type serverSentEventClock interface {
    Now() time.Time
}

type systemServerSentEventClock struct{}

func (instance systemServerSentEventClock) Now() time.Time {
    return time.Now()
}

/* WithWriteBudget makes every frame re-arm the connection's write deadline to budget from the moment it is written, and hands the same writer back; net/http otherwise arms WriteTimeout once for the whole stream. The deadline bounds a write, not the stream's life, which is the handler's to bound. The budget is typically the server's own WriteTimeout, and zero leaves the deadline as the server armed it. A writer that cannot reach the connection writes every frame as without a budget. */
func (instance *ServerSentEventWriter) WithWriteBudget(budget time.Duration) *ServerSentEventWriter {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if 0 > budget {
        budget = 0
    }

    instance.writeBudget = budget

    return instance
}

/* Send emits one event frame. An event with no data is refused, since the grammar dispatches nothing for it, and so is a field value that collapses to empty once its control bytes are removed, since an empty id resets the client's resume cursor. After a frame fails part way, every later call is refused. */
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

/* validateServerSentEvent refuses the shapes the grammar reads as something other than what the caller wrote. A negative retry is refused; zero means unset. */
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

    /* an event name with no data is refused, since dispatch never fires the named listener; an id or a retry with no data takes effect and is allowed */
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

/* rearmWriteDeadlineLocked moves the write deadline to now plus the budget before a frame is written. Without a budget, or on a writer that answered ErrNotSupported once, it does nothing. */
func (instance *ServerSentEventWriter) rearmWriteDeadlineLocked() {
    if 0 >= instance.writeBudget || true == instance.deadlineUnsupported {
        return
    }

    var clock serverSentEventClock = systemServerSentEventClock{}
    if nil != instance.clock {
        clock = instance.clock
    }

    deadlineErr := nethttp.NewResponseController(instance.writer).SetWriteDeadline(clock.Now().Add(instance.writeBudget))
    if true == errors.Is(deadlineErr, nethttp.ErrNotSupported) {
        instance.deadlineUnsupported = true
    }
}

/* a comment ends the frame, not the line, so a comment-only keepalive is observable to a client reading frame by frame; Send writes every frame whole under the lock, so a keepalive never lands inside a half-built event */
const (
    serverSentEventLineTerminator  = "\n"
    serverSentEventFrameTerminator = "\n\n"
)

/* built once: a strings.Replacer is safe for concurrent use and costly to build */
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

/* Comment writes one comment frame, ended by the blank line that terminates a frame, so a keepalive is observable to a client reading frame by frame. */
func (instance *ServerSentEventWriter) Comment(text string) error {
    return instance.writeFrame(": " + sanitizeServerSentEventField(text) + serverSentEventFrameTerminator)
}

func (instance *ServerSentEventWriter) Ping() error {
    return instance.Comment("")
}
