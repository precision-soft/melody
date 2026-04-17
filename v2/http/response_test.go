package http

import (
    "bytes"
    "errors"
    "io"
    "net/http/httptest"
    "os"
    "strings"
    "testing"
)

func TestTextResponse_WritesBodyAndStatus(t *testing.T) {
    response := TextResponse(201, "created")

    rec := httptest.NewRecorder()

    err := WriteToHttpResponseWriter(nil, nil, rec, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 201 != rec.Code {
        t.Fatalf("unexpected status")
    }

    if "created" != rec.Body.String() {
        t.Fatalf("unexpected body")
    }
}

func TestJsonResponse_WritesJson(t *testing.T) {
    response, err := JsonResponse(
        200,
        map[string]any{
            "a": "b",
        },
    )
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    rec := httptest.NewRecorder()

    err = WriteToHttpResponseWriter(nil, nil, rec, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 200 != rec.Code {
        t.Fatalf("unexpected status")
    }

    if "" == rec.Body.String() {
        t.Fatalf("expected body")
    }

    contentType := rec.Header().Get("Content-Type")
    if "" == contentType {
        t.Fatalf("expected content-type header")
    }
}

func TestClose_NilBodyReader(t *testing.T) {
    response := EmptyResponse(200)

    err := response.Close()
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }
}

func TestClose_WithCloserReader(t *testing.T) {
    tmpFile, tmpErr := os.CreateTemp("", "melody-test-close-*.txt")
    if nil != tmpErr {
        t.Fatalf("failed to create temp file: %v", tmpErr)
    }
    defer os.Remove(tmpFile.Name())

    response := &Response{
        statusCode: 200,
        headers:    nil,
        bodyReader: tmpFile,
    }

    err := response.Close()
    if nil != err {
        t.Fatalf("expected nil error from Close, got: %v", err)
    }

    secondErr := tmpFile.Close()
    if nil == secondErr {
        t.Fatalf("expected error on second close of already-closed file")
    }
}

func TestClose_WithNonCloserReader(t *testing.T) {
    reader := bytes.NewReader([]byte("hello"))

    response := &Response{
        statusCode: 200,
        headers:    nil,
        bodyReader: reader,
    }

    err := response.Close()
    if nil != err {
        t.Fatalf("expected nil error, got: %v", err)
    }
}

type failingCloser struct {
    io.Reader
}

func (instance *failingCloser) Close() error {
    return errors.New("close failed")
}

func TestClose_ReturnsErrorFromCloser(t *testing.T) {
    response := &Response{
        statusCode: 200,
        headers:    nil,
        bodyReader: &failingCloser{Reader: bytes.NewReader([]byte("data"))},
    }

    err := response.Close()
    if nil == err {
        t.Fatalf("expected error from Close")
    }
    if "close failed" != err.Error() {
        t.Fatalf("unexpected error message: %s", err.Error())
    }
}

func TestAttachmentResponse_SanitizesQuotesInFilename(t *testing.T) {
    tmpFile, tmpErr := os.CreateTemp("", "melody-test-attach-*.txt")
    if nil != tmpErr {
        t.Fatalf("failed to create temp file: %v", tmpErr)
    }
    tmpFile.Close()
    defer os.Remove(tmpFile.Name())

    response, err := AttachmentResponse(200, tmpFile.Name(), `file"name.txt`)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer response.Close()

    disposition := response.Headers().Get("Content-Disposition")
    if true == strings.Contains(disposition, `"`+`file`+`"`) {
        // fine: the wrapping quotes are expected
    }
    if true == strings.Contains(disposition, `name"`) {
        t.Fatalf("raw quote must not appear inside filename, got: %s", disposition)
    }
    if false == strings.Contains(disposition, "attachment") {
        t.Fatalf("expected attachment in Content-Disposition, got: %s", disposition)
    }
}

func TestAttachmentResponse_SanitizesNewlinesInFilename(t *testing.T) {
    tmpFile, tmpErr := os.CreateTemp("", "melody-test-attach-*.txt")
    if nil != tmpErr {
        t.Fatalf("failed to create temp file: %v", tmpErr)
    }
    tmpFile.Close()
    defer os.Remove(tmpFile.Name())

    response, err := AttachmentResponse(200, tmpFile.Name(), "file\nname\r.txt")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer response.Close()

    disposition := response.Headers().Get("Content-Disposition")
    if true == strings.Contains(disposition, "\n") {
        t.Fatalf("newline should have been stripped from filename, got: %s", disposition)
    }
    if true == strings.Contains(disposition, "\r") {
        t.Fatalf("carriage return should have been stripped from filename, got: %s", disposition)
    }
}

func TestAttachmentResponse_SanitizesBackslashInFilename(t *testing.T) {
    tmpFile, tmpErr := os.CreateTemp("", "melody-test-attach-*.txt")
    if nil != tmpErr {
        t.Fatalf("failed to create temp file: %v", tmpErr)
    }
    tmpFile.Close()
    defer os.Remove(tmpFile.Name())

    response, err := AttachmentResponse(200, tmpFile.Name(), `file\name.txt`)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer response.Close()

    disposition := response.Headers().Get("Content-Disposition")
    if true == strings.Contains(disposition, `\`) {
        t.Fatalf("backslash should have been removed from filename, got: %s", disposition)
    }
}

func TestAttachmentResponse_EmptyFilename(t *testing.T) {
    tmpFile, tmpErr := os.CreateTemp("", "melody-test-attach-*.txt")
    if nil != tmpErr {
        t.Fatalf("failed to create temp file: %v", tmpErr)
    }
    tmpFile.Close()
    defer os.Remove(tmpFile.Name())

    response, err := AttachmentResponse(200, tmpFile.Name(), "")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer response.Close()

    disposition := response.Headers().Get("Content-Disposition")
    if "attachment" != disposition {
        t.Fatalf("expected plain attachment disposition, got: %s", disposition)
    }
}

func TestAttachmentResponse_EmitsRfc5987ForNonAsciiFilename(t *testing.T) {
    tmpFile, tmpErr := os.CreateTemp("", "melody-test-attach-*.txt")
    if nil != tmpErr {
        t.Fatalf("failed to create temp file: %v", tmpErr)
    }
    tmpFile.Close()
    defer os.Remove(tmpFile.Name())

    response, err := AttachmentResponse(200, tmpFile.Name(), "raport-mărți.txt")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer response.Close()

    disposition := response.Headers().Get("Content-Disposition")
    if false == strings.Contains(disposition, `filename="`) {
        t.Fatalf("expected ASCII fallback filename, got: %s", disposition)
    }
    if false == strings.Contains(disposition, `filename*=UTF-8''`) {
        t.Fatalf("expected RFC 5987 filename* extension, got: %s", disposition)
    }
    if false == strings.Contains(disposition, "%C4%83") && false == strings.Contains(disposition, "%c4%83") {
        t.Fatalf("expected percent-encoded UTF-8 bytes for ă, got: %s", disposition)
    }
}

func TestAttachmentResponse_AsciiOnlyFilenameOmitsRfcExtension(t *testing.T) {
    tmpFile, tmpErr := os.CreateTemp("", "melody-test-attach-*.txt")
    if nil != tmpErr {
        t.Fatalf("failed to create temp file: %v", tmpErr)
    }
    tmpFile.Close()
    defer os.Remove(tmpFile.Name())

    response, err := AttachmentResponse(200, tmpFile.Name(), "report.txt")
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    defer response.Close()

    disposition := response.Headers().Get("Content-Disposition")
    if true == strings.Contains(disposition, "filename*=") {
        t.Fatalf("ASCII-only filename must not emit filename*, got: %s", disposition)
    }
    if `attachment; filename="report.txt"` != disposition {
        t.Fatalf("unexpected disposition: %s", disposition)
    }
}

func TestHtmlResponse_ContentType(t *testing.T) {
    response := HtmlResponse(200, "<h1>Hello</h1>")

    rec := httptest.NewRecorder()
    err := WriteToHttpResponseWriter(nil, nil, rec, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 200 != rec.Code {
        t.Fatalf("unexpected status code: %d", rec.Code)
    }

    contentType := rec.Header().Get("Content-Type")
    if ContentTypeTextHtml != contentType {
        t.Fatalf("unexpected content-type: %s", contentType)
    }

    if "<h1>Hello</h1>" != rec.Body.String() {
        t.Fatalf("unexpected body: %s", rec.Body.String())
    }
}

func TestJsonErrorResponse_ContainsErrorField(t *testing.T) {
    response := JsonErrorResponse(500, "something went wrong")

    rec := httptest.NewRecorder()
    err := WriteToHttpResponseWriter(nil, nil, rec, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 500 != rec.Code {
        t.Fatalf("unexpected status code: %d", rec.Code)
    }

    body := rec.Body.String()
    if false == strings.Contains(body, "something went wrong") {
        t.Fatalf("expected error message in body, got: %s", body)
    }
}
