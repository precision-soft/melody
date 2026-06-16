package http

import (
    "net/http/httptest"
    "strings"
    "testing"
)

func TestRecordingResponseWriter_FlushRecordsHeaderCommit(t *testing.T) {
    writer := newRecordingResponseWriter(httptest.NewRecorder())

    if true == writer.HeadersWritten() {
        t.Fatal("expected no headers recorded before flush")
    }

    writer.Flush()

    if false == writer.HeadersWritten() {
        t.Fatal("expected Flush to record that the response headers were committed")
    }
}

func TestRecordingResponseWriter_FlushDoesNotRecordHeaderCommitWhenUnderlyingIsNotFlusher(t *testing.T) {
    writer := newRecordingResponseWriter(&nonFlushingResponseWriter{})

    writer.Flush()

    if true == writer.HeadersWritten() {
        t.Fatal("expected Flush over a non-flushing writer not to record a header commit")
    }
}

func TestRecordingResponseWriter_WriteHeaderRecordsHeaderCommit(t *testing.T) {
    writer := newRecordingResponseWriter(httptest.NewRecorder())

    writer.WriteHeader(200)

    if false == writer.HeadersWritten() {
        t.Fatal("expected WriteHeader to record that the response headers were committed")
    }
}

func TestRecordingResponseWriter_WriteRecordsHeaderCommit(t *testing.T) {
    writer := newRecordingResponseWriter(httptest.NewRecorder())

    _, writeErr := writer.Write([]byte("body"))
    if nil != writeErr {
        t.Fatalf("expected Write to succeed, got %v", writeErr)
    }

    if false == writer.HeadersWritten() {
        t.Fatal("expected Write to record that the response headers were committed")
    }
}

func TestRecordingResponseWriter_ReadFromForwardsToUnderlyingAndRecordsCommit(t *testing.T) {
    recorder := httptest.NewRecorder()
    writer := newRecordingResponseWriter(recorder)

    written, readFromErr := writer.ReadFrom(strings.NewReader("payload"))
    if nil != readFromErr {
        t.Fatalf("expected ReadFrom to succeed, got %v", readFromErr)
    }

    if int64(len("payload")) != written {
        t.Fatalf("expected ReadFrom to report %d bytes, got %d", len("payload"), written)
    }

    if "payload" != recorder.Body.String() {
        t.Fatalf("expected ReadFrom to forward the bytes to the underlying writer, got %q", recorder.Body.String())
    }

    if false == writer.HeadersWritten() {
        t.Fatal("expected ReadFrom to record that the response headers were committed")
    }
}

func TestRecordingResponseWriter_UnwrapReturnsUnderlying(t *testing.T) {
    recorder := httptest.NewRecorder()
    writer := newRecordingResponseWriter(recorder)

    if recorder != writer.Unwrap() {
        t.Fatal("expected Unwrap to return the underlying response writer so http.ResponseController can reach it")
    }
}
