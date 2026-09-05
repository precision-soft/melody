package http

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "mime"
    nethttp "net/http"
    "net/textproto"
    neturl "net/url"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
)

const (
    ContentTypeTextPlain = "text/plain; charset=utf-8"
    ContentTypeTextHtml  = "text/html; charset=utf-8"
    ContentTypeJson      = "application/json; charset=utf-8"
)

type Response struct {
    statusCode int
    headers    nethttp.Header
    bodyReader io.Reader
}

func (instance *Response) StatusCode() int { return instance.statusCode }

func (instance *Response) SetStatusCode(statusCode int) { instance.statusCode = statusCode }

func (instance *Response) Headers() nethttp.Header { return instance.headers }

func (instance *Response) SetHeaders(headers nethttp.Header) {
    if nil == headers {
        instance.headers = nil
        return
    }

    copied := make(nethttp.Header, len(headers))
    for key, values := range headers {
        if nil == values {
            copied[key] = nil
            continue
        }

        copied[key] = append([]string{}, values...)
    }

    instance.headers = copied
}

func (instance *Response) BodyReader() io.Reader { return instance.bodyReader }

func (instance *Response) SetBodyReader(reader io.Reader) { instance.bodyReader = reader }

func (instance *Response) Close() error {
    if nil == instance.bodyReader {
        return nil
    }
    if closer, ok := instance.bodyReader.(io.Closer); true == ok {
        return closer.Close()
    }
    return nil
}

var _ httpcontract.Response = (*Response)(nil)

/* ErrorResponsePayloadDetail is the error object of the standardized error envelope: the message always, with the debug-only context and cause rendered beside it by the kernel's error renderer. */
type ErrorResponsePayloadDetail struct {
    Message string `json:"message"`
}

/* ErrorResponsePayload is the standardized error envelope every framework error body shares: the status names the answer inside the body the way the status line names it outside, the moment dates it, and the error object carries the failure itself. The kernel's error renderer adds requestId and the validation detail where it knows them. */
type ErrorResponsePayload struct {
    Status int                        `json:"status"`
    Time   string                     `json:"time"`
    Error  ErrorResponsePayloadDetail `json:"error"`
}

func NewErrorResponsePayload(statusCode int, message string, timeString string) *ErrorResponsePayload {
    return &ErrorResponsePayload{
        Status: statusCode,
        Time:   timeString,
        Error: ErrorResponsePayloadDetail{
            Message: message,
        },
    }
}

func NewResponse(statusCode int, body []byte) *Response {
    headers := make(nethttp.Header)
    headers.Set("Content-Type", ContentTypeTextPlain)

    var copiedBody []byte
    if nil != body {
        copiedBody = append([]byte{}, body...)
    }

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: bytes.NewReader(copiedBody),
    }
}

func EmptyResponse(statusCode int) *Response {
    headers := make(nethttp.Header)
    headers.Set("Content-Type", ContentTypeTextPlain)

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: nil,
    }
}

func TextResponse(statusCode int, message string) *Response {
    headers := make(nethttp.Header)
    headers.Set("Content-Type", ContentTypeTextPlain)

    data := []byte(message)

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: bytes.NewReader(data),
    }
}

func HtmlResponse(statusCode int, html string) *Response {
    headers := make(nethttp.Header)
    headers.Set("Content-Type", ContentTypeTextHtml)

    data := []byte(html)

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: bytes.NewReader(data),
    }
}

func JsonResponse(statusCode int, payload any) (*Response, error) {
    data, err := json.Marshal(payload)
    if nil != err {
        return nil, err
    }

    headers := make(nethttp.Header)
    headers.Set("Content-Type", ContentTypeJson)

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: bytes.NewReader(data),
    }, nil
}

func JsonErrorResponse(statusCode int, message string) *Response {
    payload := NewErrorResponsePayload(statusCode, message, time.Now().Format(time.RFC3339))

    response, jsonResponseErr := JsonResponse(statusCode, payload)
    if nil == jsonResponseErr {
        return response
    }

    fallbackPayload := map[string]any{
        "status": statusCode,
        "time":   time.Now().Format(time.RFC3339),
        "error": map[string]string{
            "message": message,
        },
    }

    data, marshalErr := json.Marshal(fallbackPayload)
    if nil != marshalErr {
        return TextResponse(statusCode, "internal server error")
    }

    headers := make(nethttp.Header)
    headers.Set("Content-Type", ContentTypeJson)

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: bytes.NewReader(data),
    }
}

/* FileResponse opens path exactly as given and streams it as the response body — it applies no folding, no root and no containment check, so it must never be handed a path built from client input without the caller confining it first. `os.Open("storage/invoices/" + request.Input("name"))` reads "../../../../etc/passwd" as readily as an invoice. Confine the name to a known directory before the call (reject "..", resolve symlinks and check the result stays under the root — the static file server does this for the paths it serves), or serve a fixed set of files by a lookup the client cannot steer. The returned body is the open file; the kernel closes it after the response is written. */
func FileResponse(statusCode int, path string) (*Response, error) {
    file, err := os.Open(path)
    if nil != err {
        return nil, err
    }

    headers := make(nethttp.Header)

    extension := filepath.Ext(path)
    if "" != extension {
        contentType := contentTypeByExtension(extension)
        if "" != contentType {
            headers.Set("Content-Type", contentType)
        }
    }

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: file,
    }, nil
}

/* AttachmentResponse is FileResponse with a Content-Disposition, and inherits its whole contract — no folding, no root, no containment: never hand it a path built from client input without confining it first. The confined doors below are the form built for a client-steered name. */
func AttachmentResponse(statusCode int, path string, filename string) (*Response, error) {
    response, err := FileResponse(statusCode, path)
    if nil != err {
        return nil, err
    }

    response.headers.Set("Content-Disposition", BuildContentDisposition("attachment", filename))

    return response, nil
}

/* ConfinedFileResponse serves a file selected by a name a client may steer, confined to the root directory: the name is refused when absolute or when it climbs (".."), the joined path is resolved through every symlink and checked to still lie under the resolved root, and only a regular file is answered — a directory is not a body, and a fifo would park the goroutine inside the open itself. It is the door FileResponse's own warning tells the caller to build; built here once, it cannot be built subtly wrong at every call site. The open re-resolves the already-resolved path, so what remains is the same narrow swap window the static file server documents, and nothing wider. */
func ConfinedFileResponse(statusCode int, rootDirectory string, name string) (*Response, error) {
    resolvedPath, confineErr := confineFileToRoot(rootDirectory, name)
    if nil != confineErr {
        return nil, confineErr
    }

    return FileResponse(statusCode, resolvedPath)
}

/* ConfinedAttachmentResponse is ConfinedFileResponse with a Content-Disposition, the confined twin of AttachmentResponse. */
func ConfinedAttachmentResponse(statusCode int, rootDirectory string, name string, filename string) (*Response, error) {
    response, err := ConfinedFileResponse(statusCode, rootDirectory, name)
    if nil != err {
        return nil, err
    }

    response.headers.Set("Content-Disposition", BuildContentDisposition("attachment", filename))

    return response, nil
}

func confineFileToRoot(rootDirectory string, name string) (string, error) {
    if "" == strings.TrimSpace(rootDirectory) {
        return "", exception.NewError("the file root directory may not be empty", nil, nil)
    }

    trimmedName := strings.TrimSpace(name)
    if "" == trimmedName {
        return "", exception.NewError("the file name may not be empty", nil, nil)
    }

    if true == filepath.IsAbs(trimmedName) {
        return "", exception.NewError(
            "the file name may not be absolute",
            map[string]any{
                "name": name,
            },
            nil,
        )
    }

    /* the climb is refused rather than folded away: a folded "../secret" quietly becomes a valid name, and the caller never learns a client probed the boundary */
    cleanedName := filepath.Clean(trimmedName)
    if ".." == cleanedName || true == strings.HasPrefix(cleanedName, ".."+string(os.PathSeparator)) {
        return "", exception.NewError(
            "the file name may not climb out of the root directory",
            map[string]any{
                "name": name,
            },
            nil,
        )
    }

    fullPath := filepath.Join(rootDirectory, cleanedName)

    realPath, evalErr := filepath.EvalSymlinks(fullPath)
    if nil != evalErr {
        return "", evalErr
    }

    realRoot, evalRootErr := filepath.EvalSymlinks(rootDirectory)
    if nil != evalRootErr {
        return "", evalRootErr
    }

    if realPath != realRoot && false == strings.HasPrefix(realPath, realRoot+string(os.PathSeparator)) {
        return "", exception.NewError(
            "the file resolves outside the root directory",
            map[string]any{
                "name": name,
            },
            nil,
        )
    }

    pathInfo, statErr := os.Stat(realPath)
    if nil != statErr {
        return "", statErr
    }

    if false == pathInfo.Mode().IsRegular() {
        return "", exception.NewError(
            "the confined file is not a regular file",
            map[string]any{
                "name": name,
                "mode": pathInfo.Mode().String(),
            },
            nil,
        )
    }

    return realPath, nil
}

func BuildContentDisposition(disposition string, filename string) string {
    if "" == filename {
        return disposition
    }

    asciiFallback := asciiFallbackFilename(filename)
    encoded := rfc5987EncodeFilename(filename)

    if encoded == asciiFallback {
        return fmt.Sprintf(`%s; filename="%s"`, disposition, asciiFallback)
    }

    return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, asciiFallback, encoded)
}

func asciiFallbackFilename(filename string) string {
    builder := strings.Builder{}
    builder.Grow(len(filename))

    for _, runeChar := range filename {
        switch {
        case '\\' == runeChar, '"' == runeChar, '\r' == runeChar, '\n' == runeChar:
            continue
        case 0x20 > runeChar, 0x7E < runeChar:
            builder.WriteByte('_')
        default:
            builder.WriteRune(runeChar)
        }
    }

    result := builder.String()
    if "" == result {
        return "file"
    }

    return result
}

func rfc5987EncodeFilename(filename string) string {
    builder := strings.Builder{}
    builder.Grow(len(filename))

    for _, byteChar := range []byte(filename) {
        if true == isRfc5987AttrChar(byteChar) {
            builder.WriteByte(byteChar)
            continue
        }

        builder.WriteString(fmt.Sprintf("%%%02X", byteChar))
    }

    return builder.String()
}

func isRfc5987AttrChar(byteChar byte) bool {
    switch {
    case 'A' <= byteChar && 'Z' >= byteChar:
        return true
    case 'a' <= byteChar && 'z' >= byteChar:
        return true
    case '0' <= byteChar && '9' >= byteChar:
        return true
    case '!' == byteChar, '#' == byteChar, '$' == byteChar, '&' == byteChar, '+' == byteChar, '-' == byteChar, '.' == byteChar, '^' == byteChar, '_' == byteChar, '`' == byteChar, '|' == byteChar, '~' == byteChar:
        return true
    }

    return false
}

/* RedirectResponse answers a redirect to a location WITHIN this application: an absolute target ("https://…", "mailto:…"), a scheme-relative one ("//host/…") and one carrying a backslash — which some browsers read as a slash — are refused by panic, because a location built from client input is exactly how an open redirect is minted, and the refusal makes the unsafe composition fail loudly at the first probe instead of shipping. A zero status code reads as 302. A redirect that genuinely leaves the application states it through RedirectExternalResponse, whose name is the assertion that the target is the caller's own. */
func RedirectResponse(location string, statusCode int) *Response {
    if true == isExternalRedirectLocation(location) {
        exception.Panic(
            exception.NewError(
                "the redirect location must be relative; an external target goes through RedirectExternalResponse",
                map[string]any{
                    "location": location,
                },
                nil,
            ),
        )
    }

    return RedirectExternalResponse(location, statusCode)
}

/* RedirectExternalResponse answers a redirect to any location, unguarded on purpose: calling it is the caller's assertion that the target is trusted — a fixed url, an allowlisted origin — and never raw client input. */
func RedirectExternalResponse(location string, statusCode int) *Response {
    if 0 == statusCode {
        statusCode = nethttp.StatusFound
    }

    headers := make(nethttp.Header)
    headers.Set("Location", location)

    return &Response{
        statusCode: statusCode,
        headers:    headers,
        bodyReader: nil,
    }
}

/* isExternalRedirectLocation reads the location the way a browser will: a scheme ("https:", "mailto:", "javascript:") or a leading "//" leaves the origin, and a backslash is treated as leaving too, because several browsers fold "\" to "/" while net/url does not — the disagreement is the exploit.

The reading runs on the location the header writer will emit rather than on the one the caller passed. net/textproto folds away leading and trailing spaces and tabs as it writes the field, so " //evil.example.com" reaches the browser as "//evil.example.com" while the untrimmed spelling carries neither the scheme-relative prefix nor a scheme for the checks below to find. */
func isExternalRedirectLocation(location string) bool {
    emittedLocation := textproto.TrimString(location)

    if true == strings.Contains(emittedLocation, "\\") {
        return true
    }

    if true == strings.HasPrefix(emittedLocation, "//") {
        return true
    }

    parsedLocation, parseErr := neturl.Parse(emittedLocation)
    if nil != parseErr {
        return true
    }

    return "" != parsedLocation.Scheme
}

func RedirectFound(location string) *Response { return RedirectResponse(location, nethttp.StatusFound) }

func RedirectMovedPermanently(location string) *Response {
    return RedirectResponse(location, nethttp.StatusMovedPermanently)
}

func contentTypeByExtension(extension string) string {
    contentType := mime.TypeByExtension(extension)
    if "" != contentType {
        return contentType
    }

    return fallbackContentTypeByExtension[strings.ToLower(extension)]
}

var fallbackContentTypeByExtension = map[string]string{
    ".css":   "text/css; charset=utf-8",
    ".ico":   "image/x-icon",
    ".js":    "text/javascript; charset=utf-8",
    ".json":  "application/json",
    ".map":   "application/json",
    ".mjs":   "text/javascript; charset=utf-8",
    ".otf":   "font/otf",
    ".svg":   "image/svg+xml",
    ".ttf":   "font/ttf",
    ".wasm":  "application/wasm",
    ".webp":  "image/webp",
    ".woff":  "font/woff",
    ".woff2": "font/woff2",
}
