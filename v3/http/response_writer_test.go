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

type panickingHeaderWriter struct {
    nethttp.ResponseWriter
}

func (instance *panickingHeaderWriter) WriteHeader(statusCode int) {
    panic("invalid WriteHeader code")
}

func TestRecordingResponseWriter_APanickingDelegateLeavesTheCommitFlagFalse(t *testing.T) {
    recording := newRecordingResponseWriter(&panickingHeaderWriter{httptest.NewRecorder()})

    func() {
        defer func() { _ = recover() }()
        recording.WriteHeader(99)
    }()

    if true == recording.HeadersWritten() {
        t.Fatalf("expected a delegate that panicked before committing to leave the flag false; a lying flag made the recovery skip its 500 and the client received an implicit empty 200")
    }
}

func TestRecordingResponseWriter_ReadFromLeavesTheCommitFlagFalseWhenTheSourceFailsBeforeTheFirstByte(t *testing.T) {
    recorder := httptest.NewRecorder()
    writer := newRecordingResponseWriter(recorder)

    _, readFromErr := writer.ReadFrom(&failingAtFirstByteReader{})
    if nil == readFromErr {
        t.Fatal("expected the failing source's error to propagate")
    }

    if true == writer.HeadersWritten() {
        t.Fatal("expected a copy that moved no byte to record no commit")
    }

    if 0 != writer.CommittedStatusCode() {
        t.Fatalf("expected no committed status, got %d", writer.CommittedStatusCode())
    }
}

type failingAtFirstByteReader struct{}

func (instance *failingAtFirstByteReader) Read(buffer []byte) (int, error) {
    return 0, nethttp.ErrBodyReadAfterClose
}

func TestRecordingResponseWriter_CommittedStatusCodeAnswersTheExplicitStatus(t *testing.T) {
    writer := newRecordingResponseWriter(httptest.NewRecorder())

    writer.WriteHeader(nethttp.StatusTeapot)

    if nethttp.StatusTeapot != writer.CommittedStatusCode() {
        t.Fatalf("expected the explicitly written status, got %d", writer.CommittedStatusCode())
    }
}

func TestRecordingResponseWriter_CommittedStatusCodeAnswersTheImplicitTwoHundredOnFirstWrite(t *testing.T) {
    writer := newRecordingResponseWriter(httptest.NewRecorder())

    _, writeErr := writer.Write([]byte("body"))
    if nil != writeErr {
        t.Fatalf("expected the write to succeed, got %v", writeErr)
    }

    if nethttp.StatusOK != writer.CommittedStatusCode() {
        t.Fatalf("expected the implicit 200, got %d", writer.CommittedStatusCode())
    }
}

func TestRecordingResponseWriter_APanickingFlushLeavesTheCommitFlagFalse(t *testing.T) {
    writer := newRecordingResponseWriter(&panickingFlushWriter{httptest.NewRecorder()})

    func() {
        defer func() { _ = recover() }()
        writer.Flush()
    }()

    if true == writer.HeadersWritten() {
        t.Fatal("expected a flush that panicked before committing to leave the flag false")
    }
}

type panickingFlushWriter struct {
    *httptest.ResponseRecorder
}

func (instance *panickingFlushWriter) Flush() {
    panic("flush died before committing")
}

func TestWriteToHttpResponseWriter_RefusesAStatusOutsideTheWritableRangeByName(t *testing.T) {
    recorder := httptest.NewRecorder()

    response := NewResponse(1000, []byte("body"))

    writeErr := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil == writeErr {
        t.Fatal("expected the out-of-range status to be refused")
    }

    if false == strings.Contains(writeErr.Error(), "status code is out of range") {
        t.Fatalf("expected the refusal to name the rule, got %q", writeErr.Error())
    }

    if 0 != recorder.Body.Len() {
        t.Fatalf("expected nothing written to the delegate, got %d bytes", recorder.Body.Len())
    }
}

func TestWriteToHttpResponseWriter_ARefusedStatusLeavesNoHeaderOnTheWriter(t *testing.T) {
    recorder := httptest.NewRecorder()

    response := NewResponse(1000, []byte("body"))
    response.Headers().Set("Set-Cookie", "session=abc")
    response.Headers().Set("X-Refused", "left-behind")

    writeErr := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil == writeErr {
        t.Fatal("expected the out-of-range status to be refused")
    }

    if "" != recorder.Header().Get("Set-Cookie") {
        t.Fatalf("expected the refused response to leave no cookie behind, got %q", recorder.Header().Get("Set-Cookie"))
    }

    if "" != recorder.Header().Get("X-Refused") {
        t.Fatalf("expected the refused response to leave no header behind, got %q", recorder.Header().Get("X-Refused"))
    }
}

func TestWriteToHttpResponseWriter_RefusesAStatusBelowTheWritableRange(t *testing.T) {
    recorder := httptest.NewRecorder()

    response := NewResponse(99, []byte("body"))
    response.Headers().Set("X-Refused", "left-behind")

    writeErr := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil == writeErr {
        t.Fatal("expected the below-range status to be refused")
    }

    if "" != recorder.Header().Get("X-Refused") {
        t.Fatalf("expected the refused response to leave no header behind, got %q", recorder.Header().Get("X-Refused"))
    }
}

func TestWriteToHttpResponseWriter_AnAcceptedStatusStillCarriesItsHeaders(t *testing.T) {
    recorder := httptest.NewRecorder()

    response := NewResponse(nethttp.StatusCreated, []byte("body"))
    response.Headers().Set("X-Accepted", "carried")

    writeErr := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != writeErr {
        t.Fatalf("unexpected write error: %v", writeErr)
    }

    if "carried" != recorder.Header().Get("X-Accepted") {
        t.Fatalf("expected the accepted response's header to reach the writer, got %q", recorder.Header().Get("X-Accepted"))
    }

    if nethttp.StatusCreated != recorder.Code {
        t.Fatalf("expected the accepted status on the connection, got %d", recorder.Code)
    }
}

func TestWriteToHttpResponseWriter_KeepsACookieTheHandlerWroteOnTheWriter(t *testing.T) {
    recorder := httptest.NewRecorder()
    nethttp.SetCookie(recorder, &nethttp.Cookie{Name: "handler_cookie", Value: "kept", Path: "/"})

    response := NewResponse(nethttp.StatusOK, []byte("body"))
    SetCookie(response, &nethttp.Cookie{Name: "melody_session", Value: "abc", Path: "/"})

    writeErr := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != writeErr {
        t.Fatalf("unexpected error: %v", writeErr)
    }

    emitted := recorder.Header().Values("Set-Cookie")
    if 2 != len(emitted) {
        t.Fatalf("expected both cookies on the wire, got %v", emitted)
    }

    handlerCookieEmitted := false
    responseCookieEmitted := false
    for _, cookieLine := range emitted {
        if true == strings.HasPrefix(cookieLine, "handler_cookie=kept") {
            handlerCookieEmitted = true
        }
        if true == strings.HasPrefix(cookieLine, "melody_session=abc") {
            responseCookieEmitted = true
        }
    }

    if false == handlerCookieEmitted {
        t.Fatalf("expected the handler's own cookie to survive, got %v", emitted)
    }

    if false == responseCookieEmitted {
        t.Fatalf("expected the response's cookie to be emitted, got %v", emitted)
    }
}

func TestWriteToHttpResponseWriter_StillReplacesANonCookieHeaderTheResponseNames(t *testing.T) {
    recorder := httptest.NewRecorder()
    recorder.Header().Set(HeaderRequestId, "from-the-writer")

    response := NewResponse(nethttp.StatusOK, []byte("body"))
    response.Headers().Set(HeaderRequestId, "from-the-response")

    writeErr := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != writeErr {
        t.Fatalf("unexpected error: %v", writeErr)
    }

    emitted := recorder.Header().Values(HeaderRequestId)
    if 1 != len(emitted) || "from-the-response" != emitted[0] {
        t.Fatalf("expected the response to own the header it names, got %v", emitted)
    }
}

type intermediateResponseWriterWrapper struct {
    nethttp.ResponseWriter
}

func (instance *intermediateResponseWriterWrapper) Unwrap() nethttp.ResponseWriter {
    return instance.ResponseWriter
}

type flushCountingResponseRecorder struct {
    *httptest.ResponseRecorder
    flushes int
}

func (instance *flushCountingResponseRecorder) Flush() {
    instance.flushes++
    instance.ResponseRecorder.Flush()
}

func TestRecordingResponseWriter_FlushReachesTheConnectionThroughAnIntermediateWrapper(t *testing.T) {
    connection := &flushCountingResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
    writer := newRecordingResponseWriter(&intermediateResponseWriterWrapper{ResponseWriter: connection})

    writer.Flush()
    writer.Flush()

    if 2 != connection.flushes {
        t.Fatalf("expected both flushes to reach the connection, got %d", connection.flushes)
    }

    if false == writer.HeadersWritten() {
        t.Fatal("expected the flush that reached the connection to record the commit")
    }
}
