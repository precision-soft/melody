package http

import (
    "io"
    nethttp "net/http"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
)

type SseEvent struct {
    Id    string
    Event string
    Data  string
    Retry int
}

func NewSseWriter(writer nethttp.ResponseWriter) (*SseWriter, error) {
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

    return &SseWriter{
        writer:  writer,
        flusher: flusher,
    }, nil
}

type SseWriter struct {
    writer  nethttp.ResponseWriter
    flusher nethttp.Flusher
}

func (instance *SseWriter) Send(event SseEvent) error {
    var builder strings.Builder

    if "" != event.Id {
        builder.WriteString("id: ")
        builder.WriteString(event.Id)
        builder.WriteString("\n")
    }

    if "" != event.Event {
        builder.WriteString("event: ")
        builder.WriteString(event.Event)
        builder.WriteString("\n")
    }

    if 0 < event.Retry {
        builder.WriteString("retry: ")
        builder.WriteString(strconv.Itoa(event.Retry))
        builder.WriteString("\n")
    }

    for _, line := range strings.Split(event.Data, "\n") {
        builder.WriteString("data: ")
        builder.WriteString(line)
        builder.WriteString("\n")
    }

    builder.WriteString("\n")

    _, writeErr := io.WriteString(instance.writer, builder.String())
    if nil != writeErr {
        return writeErr
    }

    instance.flusher.Flush()

    return nil
}

func (instance *SseWriter) Comment(text string) error {
    _, writeErr := io.WriteString(instance.writer, ": "+text+"\n\n")
    if nil != writeErr {
        return writeErr
    }

    instance.flusher.Flush()

    return nil
}
