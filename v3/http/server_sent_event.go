package http

import (
    "io"
    nethttp "net/http"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

type ServerSentEvent struct {
    Id    string
    Event string
    Data  string
    Retry int
}

func NewServerSentEventWriter(writer nethttp.ResponseWriter) (*ServerSentEventWriter, error) {
    flusher, isFlusher := writer.(nethttp.Flusher)
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

type ServerSentEventWriter struct {
    writer  nethttp.ResponseWriter
    flusher nethttp.Flusher
}

func (instance *ServerSentEventWriter) Send(event ServerSentEvent) error {
    var builder strings.Builder

    if "" != event.Id {
        builder.WriteString("id: ")
        builder.WriteString(sanitizeServerSentEventId(event.Id))
        builder.WriteString("\n")
    }

    if "" != event.Event {
        builder.WriteString("event: ")
        builder.WriteString(sanitizeServerSentEventField(event.Event))
        builder.WriteString("\n")
    }

    if 0 < event.Retry {
        builder.WriteString("retry: ")
        builder.WriteString(strconv.Itoa(event.Retry))
        builder.WriteString("\n")
    }

    if "" != event.Data {
        normalizedData := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(event.Data)
        for _, line := range strings.Split(normalizedData, "\n") {
            builder.WriteString("data: ")
            builder.WriteString(line)
            builder.WriteString("\n")
        }
    }

    builder.WriteString("\n")

    _, writeErr := io.WriteString(instance.writer, builder.String())
    if nil != writeErr {
        return writeErr
    }

    instance.flusher.Flush()

    return nil
}

func sanitizeServerSentEventField(value string) string {
    return strings.NewReplacer("\r", "", "\n", "").Replace(value)
}

func sanitizeServerSentEventId(value string) string {
    return strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(value)
}

func (instance *ServerSentEventWriter) Comment(text string) error {
    _, writeErr := io.WriteString(instance.writer, ": "+sanitizeServerSentEventField(text)+"\n\n")
    if nil != writeErr {
        return writeErr
    }

    instance.flusher.Flush()

    return nil
}

func (instance *ServerSentEventWriter) Ping() error {
    return instance.Comment("")
}
