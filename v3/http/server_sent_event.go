package http

import (
    "io"
    nethttp "net/http"
    "strconv"
    "strings"
    "sync"

    "github.com/precision-soft/melody/v3/exception"
)

type ServerSentEvent struct {
    Id    string
    Event string
    Data  string
    Retry int
}

/* NewServerSentEventWriter refuses an already-committed response or a delegate that cannot flush before committing event-stream headers. Its frame writes are concurrency-safe, but the writer must not outlive the handler. */
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

    flusher, isFlusher := writer.(nethttp.Flusher)
    if false == isFlusher {
        return nil, false
    }

    return flusher, true
}

type ServerSentEventWriter struct {
    mutex   sync.Mutex
    writer  nethttp.ResponseWriter
    flusher nethttp.Flusher
    broken  bool
}

/* Send emits one event frame. An event carrying no data is refused rather than written: the event stream grammar dispatches nothing for a frame with an empty data buffer, so a caller naming an event type and no payload sent a frame the browser is required to discard and had no way to find out. A field value that would collapse to empty once its control bytes are removed is refused for the same reason — an id rewritten to the empty string silently resets the client's resume cursor.

   A frame that failed partway leaves bytes on the wire that no later frame can repair, so the writer refuses every subsequent call rather than appending a well-formed frame onto a torn one. */
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
        normalizedData := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(event.Data)
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

const (
    serverSentEventLineTerminator  = "\n"
    serverSentEventFrameTerminator = "\n\n"
)

func sanitizeServerSentEventField(value string) string {
    return strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(value)
}

func sanitizeServerSentEventId(value string) string {
    return strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(value)
}

/* Comment writes one comment frame, ended by the blank line that terminates a frame: a comment-only keepalive is observable to a client reading frame by frame only through that blank line, and the half-built-event hazard a bare newline would avoid cannot arise here, since Send composes every frame whole and writes it under the lock. */
func (instance *ServerSentEventWriter) Comment(text string) error {
    return instance.writeFrame(": " + sanitizeServerSentEventField(text) + serverSentEventFrameTerminator)
}

func (instance *ServerSentEventWriter) Ping() error {
    return instance.Comment("")
}
