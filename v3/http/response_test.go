package http

import (
    "bytes"
    "encoding/json"
    "errors"
    "io"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestTextResponse_WritesBodyAndStatus(t *testing.T) {
    response := TextResponse(201, "created")

    recorder := httptest.NewRecorder()

    err := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 201 != recorder.Code {
        t.Fatalf("unexpected status")
    }

    if "created" != recorder.Body.String() {
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

    recorder := httptest.NewRecorder()

    err = WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 200 != recorder.Code {
        t.Fatalf("unexpected status")
    }

    if "" == recorder.Body.String() {
        t.Fatalf("expected body")
    }

    contentType := recorder.Header().Get("Content-Type")
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

    recorder := httptest.NewRecorder()
    err := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 200 != recorder.Code {
        t.Fatalf("unexpected status code: %d", recorder.Code)
    }

    contentType := recorder.Header().Get("Content-Type")
    if ContentTypeTextHtml != contentType {
        t.Fatalf("unexpected content-type: %s", contentType)
    }

    if "<h1>Hello</h1>" != recorder.Body.String() {
        t.Fatalf("unexpected body: %s", recorder.Body.String())
    }
}

func TestJsonErrorResponse_ContainsErrorField(t *testing.T) {
    response := JsonErrorResponse(500, "something went wrong")

    recorder := httptest.NewRecorder()
    err := WriteToHttpResponseWriter(nil, nil, recorder, response)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 500 != recorder.Code {
        t.Fatalf("unexpected status code: %d", recorder.Code)
    }

    body := recorder.Body.String()
    if false == strings.Contains(body, "something went wrong") {
        t.Fatalf("expected error message in body, got: %s", body)
    }
}

/* JsonErrorResponse is the constructor the security entry points and every fallback answer through, so its body is the standardized envelope: the status inside the body the way the status line carries it outside, the moment, and the error object with the message */
func TestJsonErrorResponse_CarriesTheStandardizedEnvelope(t *testing.T) {
    response := JsonErrorResponse(429, "too many requests")

    envelope := struct {
        Status int    `json:"status"`
        Time   string `json:"time"`
        Error  struct {
            Message string `json:"message"`
        } `json:"error"`
    }{}

    body := readResponseBody(t, response)
    if unmarshalErr := json.Unmarshal([]byte(body), &envelope); nil != unmarshalErr {
        t.Fatalf("expected the standardized error envelope, got %s (%v)", body, unmarshalErr)
    }

    if 429 != envelope.Status {
        t.Fatalf("expected the envelope to name the status, got %d in %s", envelope.Status, body)
    }

    if "too many requests" != envelope.Error.Message {
        t.Fatalf("expected the error object to carry the message, got %q in %s", envelope.Error.Message, body)
    }

    if "" == envelope.Time {
        t.Fatalf("expected the envelope to date the answer, got %s", body)
    }
}

func TestContentTypeByExtension_ResolvesIcoAndIsCaseInsensitive(t *testing.T) {
    icoType := contentTypeByExtension(".ico")
    if false == strings.HasPrefix(icoType, "image/") {
        t.Fatalf("expected an image content type for .ico, got %q", icoType)
    }

    if contentTypeByExtension(".ICO") != icoType {
        t.Fatalf("expected case-insensitive resolution for .ICO, got %q want %q", contentTypeByExtension(".ICO"), icoType)
    }

    if "" == contentTypeByExtension(".svg") {
        t.Fatalf("expected a content type for .svg, got empty")
    }
}

/* the refusal is what makes the unsafe composition fail at the first probe: a location built from client input is how an open redirect is minted, and each of the three shapes below is a way a browser leaves the origin — a scheme, a scheme-relative slash pair, and the backslash several browsers fold to a slash while net/url does not */
func TestRedirectResponse_RefusesTheLocationsThatLeaveTheApplication(t *testing.T) {
    for _, location := range []string{
        "https://evil.example.com/",
        "http://evil.example.com",
        "mailto:someone@example.com",
        "javascript:alert(1)",
        "//evil.example.com/path",
        "/\\evil.example.com",
        "\\/evil.example.com",
        "http://evil.example.com\\@good.example.com",
    } {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("expected the location %q to be refused", location)
                }
            }()

            _ = RedirectResponse(location, 0)
        }()
    }
}

func TestRedirectResponse_AnswersTheRelativeLocations(t *testing.T) {
    for _, location := range []string{"/login", "/products?page=2", "login", "../up", "/a/b#frag"} {
        response := RedirectResponse(location, 0)

        if 302 != response.StatusCode() {
            t.Fatalf("expected the zero status to read as 302 for %q, got %d", location, response.StatusCode())
        }
        if location != response.Headers().Get("Location") {
            t.Fatalf("expected the location %q, got %q", location, response.Headers().Get("Location"))
        }
    }
}

/* the external door is the caller's assertion, so it must answer exactly what the guarded one refuses */
func TestRedirectExternalResponse_AnswersAnAbsoluteLocation(t *testing.T) {
    response := RedirectExternalResponse("https://partner.example.com/checkout", 0)

    if "https://partner.example.com/checkout" != response.Headers().Get("Location") {
        t.Fatalf("expected the absolute location, got %q", response.Headers().Get("Location"))
    }
}

func TestConfinedFileResponse_ServesANameUnderTheRootAndNothingOutsideIt(t *testing.T) {
    rootDirectory := t.TempDir()
    outsideDirectory := t.TempDir()

    writeErr := os.WriteFile(rootDirectory+"/invoice.txt", []byte("the invoice"), 0o644)
    if nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }
    writeErr = os.WriteFile(outsideDirectory+"/secret.txt", []byte("the secret"), 0o644)
    if nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }

    response, serveErr := ConfinedFileResponse(200, rootDirectory, "invoice.txt")
    if nil != serveErr {
        t.Fatalf("expected the contained name to be served, got %v", serveErr)
    }
    body, _ := io.ReadAll(response.BodyReader())
    if "the invoice" != string(body) {
        t.Fatalf("expected the invoice body, got %q", string(body))
    }

    refused := []string{
        "../" + filepath.Base(outsideDirectory) + "/secret.txt",
        "..",
        "/etc/passwd",
        "",
        "  ",
    }
    for _, name := range refused {
        _, refuseErr := ConfinedFileResponse(200, rootDirectory, name)
        if nil == refuseErr {
            t.Fatalf("expected the name %q to be refused", name)
        }
    }
}

/* the symlink is the escape the textual checks cannot see: the name is clean, the join is under the root, and the target is not */
func TestConfinedFileResponse_ASymlinkPointingOutsideTheRootIsRefused(t *testing.T) {
    rootDirectory := t.TempDir()
    outsideDirectory := t.TempDir()

    writeErr := os.WriteFile(outsideDirectory+"/secret.txt", []byte("the secret"), 0o644)
    if nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }

    symlinkErr := os.Symlink(outsideDirectory+"/secret.txt", rootDirectory+"/innocent.txt")
    if nil != symlinkErr {
        t.Fatalf("symlink error: %v", symlinkErr)
    }

    _, refuseErr := ConfinedFileResponse(200, rootDirectory, "innocent.txt")
    if nil == refuseErr {
        t.Fatalf("expected the escaping symlink to be refused")
    }
    if false == strings.Contains(refuseErr.Error(), "outside the root directory") {
        t.Fatalf("expected the refusal to name the confinement, got %v", refuseErr)
    }
}

func TestConfinedAttachmentResponse_CarriesTheDispositionOverTheConfinedFile(t *testing.T) {
    rootDirectory := t.TempDir()

    writeErr := os.WriteFile(rootDirectory+"/report.csv", []byte("a,b"), 0o644)
    if nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }

    response, serveErr := ConfinedAttachmentResponse(200, rootDirectory, "report.csv", "report.csv")
    if nil != serveErr {
        t.Fatalf("expected the attachment, got %v", serveErr)
    }

    if "" == response.Headers().Get("Content-Disposition") {
        t.Fatalf("expected the content disposition header")
    }
}
