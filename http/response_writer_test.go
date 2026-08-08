package http

import (
    nethttp "net/http"
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

/* WriteToHttpResponseWriter is a public door, so a nil pointer of a response type boxed into the contract
reaches it and passes a plain comparison. */
func TestWriteToHttpResponseWriter_ReadsATypedNilResponseAsAbsent(t *testing.T) {
    var unassignedResponse *Response

    recorder := httptest.NewRecorder()

    err := WriteToHttpResponseWriter(nil, nil, recorder, unassignedResponse)
    if nil != err {
        t.Fatalf("expected no error, got %v", err)
    }

    if 200 != recorder.Code || "" != recorder.Body.String() {
        t.Fatalf("expected nothing written, got status %d body %q", recorder.Code, recorder.Body.String())
    }
}

func TestWriteToHttpResponseWriter_ResponseOwnedKeyReplacesTheWriterValue(t *testing.T) {
    recorder := httptest.NewRecorder()
    recorder.Header().Set(HeaderRequestId, "writer-id")
    recorder.Header().Set("X-Writer-Only", "kept")

    response := TextResponse(nethttp.StatusOK, "body")
    response.Headers().Set(HeaderRequestId, "response-id")

    writeErr := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    requestIdValues := recorder.Result().Header.Values(HeaderRequestId)
    if 1 != len(requestIdValues) || "response-id" != requestIdValues[0] {
        t.Fatalf("expected the response to own the key it names, got %v", requestIdValues)
    }

    if "kept" != recorder.Result().Header.Get("X-Writer-Only") {
        t.Fatalf("expected a key the response does not name to keep the writer's value")
    }
}
