package http

import (
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

/* NewServerSentEventWriter commits the event-stream headers and returns the writer that emits frames onto them. Two refusals stand in front of the commit, because after it nothing can be answered any more — writeResponse skips a committed stream and the recovery guard declines to write a 500 over it:

   the response must not already be committed, or the frames would be appended to whatever body is in flight and the only trace would be net/http's own "superfluous WriteHeader" line on the process's stderr, outside the journal entirely; and the connection must really support streaming. The capability probe reads through the kernel's recording writer rather than at it: that wrapper always carries a Flush method, so an assertion at the wrapper succeeded even when the delegate underneath could not flush at all — the refusal was dead code for every in-framework caller, and the handler went on to subscribe and write events into a buffer nothing would ever flush.

   The returned writer is safe for concurrent use: the natural shape of an event stream is a handler emitting events beside a ticker emitting keepalives, and a net/http ResponseWriter is not safe for concurrent use, so two unsynchronized frames interleave into one corrupt frame with no error anywhere. It must not outlive the handler, which is what the hub exists to make unnecessary. */
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

/* streamingFlusherOf finds the flusher that actually reaches the connection, unwrapping the chain of ResponseWriter wrappers the way http.ResponseController does. A wrapper that forwards Flush only when its own delegate can flush — the kernel's recording writer is exactly that — answers a type assertion affirmatively while flushing nothing. */
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

    /* the flush is issued through the OUTERMOST writer so every wrapper still records the commit it is there to record — the kernel's recorder learns the stream was committed, and the access log reports the status the client received rather than zero */
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
}

/* serverSentEventClock is the instant a frame's deadline is counted from; an interface rather than a func value so the writer stays comparable, the way it was before the budget existed — a func field would have made a value that was comparable stop being one, an incompatible change of the published surface. */
type serverSentEventClock interface {
    Now() time.Time
}

type systemServerSentEventClock struct{}

func (instance systemServerSentEventClock) Now() time.Time {
    return time.Now()
}

/* WithWriteBudget makes every frame re-arm the connection's write deadline, budget from the moment the frame is written, and hands the same writer back. net/http arms a server's WriteTimeout ONCE, absolute from the moment the request line was read: on a stream that lives longer than that, every write from then on fails, the handler learns it only when the next event arrives, and that event — the first after the deadline — is the one lost, on a connection the client still believes open. Re-armed per frame the deadline means what a stream needs it to mean: a client that stops reading is still cut, budget after the LAST frame it did not take, and a stream that keeps writing keeps living. The budget a handler hands in is the server's own WriteTimeout, read off the request's server; zero leaves the deadline as the server armed it.

   The deadline is set through a ResponseController, so it reaches the connection through whatever wrapped the writer, the kernel's recording writer included. A writer that cannot be unwrapped to the connection answers ErrNotSupported, and then every frame is written as it was before this door existed — the re-arming is a capability of the connection, and a writer without it is not a broken stream. */
func (instance *ServerSentEventWriter) WithWriteBudget(budget time.Duration) *ServerSentEventWriter {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if 0 > budget {
        budget = 0
    }

    instance.writeBudget = budget

    return instance
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

/* validateServerSentEvent refuses the shapes the grammar reads as something other than what the caller wrote. A negative retry is refused rather than dropped: the field exists to instruct the client's reconnection delay, and a computed backoff that came out negative is a unit-confusion fault whose silent drop is indistinguishable from never setting it. A zero retry is the field's own zero value and means unset — a caller asking for an immediate reconnect names one millisecond. */
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

    /* an event NAME with no data is refused: the grammar returns from dispatch the moment the data buffer is empty, so the listener the caller named never fires and the caller had no way to find out. An id or a retry with no data is not refused — both take effect before that return, so a checkpoint frame and a reconnection-delay frame are deliberate spellings. */
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

/* rearmWriteDeadlineLocked moves the connection's write deadline to now plus the budget before a frame is written; without a budget, or on a writer the controller cannot reach the connection through, it does nothing and the frame goes out under the deadline the server armed. */
func (instance *ServerSentEventWriter) rearmWriteDeadlineLocked() {
    if 0 >= instance.writeBudget {
        return
    }

    var clock serverSentEventClock = systemServerSentEventClock{}
    if nil != instance.clock {
        clock = instance.clock
    }

    _ = nethttp.NewResponseController(instance.writer).SetWriteDeadline(clock.Now().Add(instance.writeBudget))
}

/* the two terminators of the grammar, named so each site says which one it ends with. A comment deliberately ends the FRAME and not merely the line: the blank line is what makes a comment-only keepalive observable to a client that reads frame by frame, which is the whole point of the preamble a stream flushes at subscription time — without it a client cannot tell a live stream from a hung one. The hazard a single newline would avoid, a keepalive landing between the fields of a half-built event and dispatching it, cannot arise here: Send composes every frame whole and writes it under the lock, so nothing is ever buffered when a comment runs. */
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
