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

/* NewServerSentEventWriter commits the event-stream headers and returns the writer that emits frames
   onto them. Two refusals stand in front of the commit, because after it nothing can be answered any
   more — writeResponse skips a committed stream and the recovery guard declines to write a 500 over
   it:

   the response must not already be committed, or the frames would be appended to whatever body is in
   flight and the only trace would be net/http's own "superfluous WriteHeader" line on the process's
   stderr, outside the journal entirely; and the connection must really support streaming. The
   capability probe reads through the kernel's recording writer rather than at it: that wrapper always
   carries a Flush method, so an assertion at the wrapper succeeded even when the delegate underneath
   could not flush at all — the refusal was dead code for every in-framework caller, and the handler
   went on to subscribe and write events into a buffer nothing would ever flush.

   The returned writer is safe for concurrent use: the natural shape of an event stream is a handler
   emitting events beside a ticker emitting keepalives, and a net/http ResponseWriter is not safe for
   concurrent use, so two unsynchronized frames interleave into one corrupt frame with no error
   anywhere. It must not outlive the handler, which is what the hub exists to make unnecessary. */
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

/* streamingFlusherOf finds the flusher that actually reaches the connection, unwrapping the chain of
   ResponseWriter wrappers the way http.ResponseController does. A wrapper that forwards Flush only
   when its own delegate can flush — the kernel's recording writer is exactly that — answers a type
   assertion affirmatively while flushing nothing. */
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

    /* the flush is issued through the OUTERMOST writer so every wrapper still records the commit it
       is there to record — the kernel's recorder learns the stream was committed, and the access log
       reports the status the client received rather than zero */
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

/* Send emits one event frame. An event carrying no data is refused rather than written: the event
   stream grammar dispatches nothing for a frame with an empty data buffer, so a caller naming an
   event type and no payload sent a frame the browser is required to discard and had no way to find
   out. A field value that would collapse to empty once its control bytes are removed is refused for
   the same reason — an id rewritten to the empty string silently resets the client's resume cursor.

   A frame that failed partway leaves bytes on the wire that no later frame can repair, so the writer
   refuses every subsequent call rather than appending a well-formed frame onto a torn one. */
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

/* validateServerSentEvent refuses the shapes the grammar reads as something other than what the
   caller wrote. A negative retry is refused rather than dropped: the field exists to instruct the
   client's reconnection delay, and a computed backoff that came out negative is a unit-confusion
   fault whose silent drop is indistinguishable from never setting it. A zero retry is the field's own
   zero value and means unset — a caller asking for an immediate reconnect names one millisecond. */
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

    /* an event NAME with no data is refused: the grammar returns from dispatch the moment the data
       buffer is empty, so the listener the caller named never fires and the caller had no way to find
       out. An id or a retry with no data is not refused — both take effect before that return, so a
       checkpoint frame and a reconnection-delay frame are deliberate spellings. */
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

/* the two terminators of the grammar, named so each site says which one it ends with. A comment
   deliberately ends the FRAME and not merely the line: the blank line is what makes a comment-only
   keepalive observable to a client that reads frame by frame, which is the whole point of the preamble
   a stream flushes at subscription time — without it a client cannot tell a live stream from a hung
   one. The hazard a single newline would avoid, a keepalive landing between the fields of a half-built
   event and dispatching it, cannot arise here: Send composes every frame whole and writes it under the
   lock, so nothing is ever buffered when a comment runs. */
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

/* Comment writes one comment line. It ends with a single newline and not two: a comment is not an
   event, and the second newline is the blank line that dispatches whatever fields are buffered — a
   keepalive issued between an event's fields would have dispatched that event half-built. */
func (instance *ServerSentEventWriter) Comment(text string) error {
    return instance.writeFrame(": " + sanitizeServerSentEventField(text) + serverSentEventFrameTerminator)
}

func (instance *ServerSentEventWriter) Ping() error {
    return instance.Comment("")
}
