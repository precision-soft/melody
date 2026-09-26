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

/* SetHeaders stores a copy of the map, or nil when handed nil, so Headers may answer nil; every writer of a response asks before it writes. */
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

/* ErrorResponsePayloadDetail is the error object of the standardized error envelope: the message, with the debug-only context and cause beside it. */
type ErrorResponsePayloadDetail struct {
    Message string `json:"message"`
}

/* ErrorResponsePayload is the standardized envelope of every framework error body: the status, the moment and the error object. The kernel's error renderer adds requestId and the validation detail where it knows them. */
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

/* FileResponse opens path exactly as given and streams it as the body, with no folding, no root and no containment check, so a path built from client input must be confined first; ConfinedFileResponse is the door for that. The body is the open file, which the kernel closes after the response is written. */
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

/* AttachmentResponse is FileResponse with a Content-Disposition and inherits its contract: never a path built from client input without confining it first. */
func AttachmentResponse(statusCode int, path string, filename string) (*Response, error) {
    response, err := FileResponse(statusCode, path)
    if nil != err {
        return nil, err
    }

    response.headers.Set("Content-Disposition", BuildContentDisposition("attachment", filename))

    return response, nil
}

/* ConfinedFileResponse serves a file selected by a name a client may steer, confined to root: an absolute or climbing name is refused, the joined path is resolved through every symlink and must stay under the resolved root, and only a regular file is answered. What remains is the narrow swap window between the resolution and the open. */
func ConfinedFileResponse(statusCode int, rootDirectory string, name string) (*Response, error) {
    resolvedPath, confineErr := confineFileToRoot(rootDirectory, name)
    if nil != confineErr {
        return nil, confineErr
    }

    return FileResponse(statusCode, resolvedPath)
}

/* ConfinedAttachmentResponse is ConfinedFileResponse with a Content-Disposition. */
func ConfinedAttachmentResponse(statusCode int, rootDirectory string, name string, filename string) (*Response, error) {
    response, err := ConfinedFileResponse(statusCode, rootDirectory, name)
    if nil != err {
        return nil, err
    }

    response.headers.Set("Content-Disposition", BuildContentDisposition("attachment", filename))

    return response, nil
}

/* confineFileToRoot resolves a name under a root and refuses an absolute name, a climb, a symlink resolving outside and anything not a regular file. dirFileSystem.Open in the static package keeps its own containment under a different contract; a change to what "outside the root" means is made in both. */
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

    /* the climb is refused rather than folded away */
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

    /* the containment is read on the relative path rather than as a textual prefix, since "." resolves names without a "./" and "/" would demand "//" */
    relativePath, relativeErr := filepath.Rel(realRoot, realPath)
    if nil != relativeErr || ".." == relativePath || true == strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) {
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

/* RedirectResponse answers a redirect to a location within this application: an absolute target, a scheme-relative one and one carrying a backslash are refused by panic, since a location built from client input is how an open redirect is minted. A zero status reads as 302. RedirectExternalResponse states a redirect that leaves the application. */
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

/* RedirectExternalResponse answers a redirect to any location, unguarded: calling it asserts the target is trusted, never raw client input. */
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

/* isExternalRedirectLocation reads the location as a browser will: a scheme or a leading "//" leaves the origin, and so does a backslash, which several browsers fold to "/". It reads the location with the spaces and tabs net/textproto trims when it writes the field. */
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
