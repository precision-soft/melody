package http

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "mime"
    nethttp "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    httpcontract "github.com/precision-soft/melody/http/contract"
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

type ErrorResponsePayload struct {
    Error string `json:"error"`
    Time  string `json:"time"`
}

func NewErrorResponsePayload(message string, timeString string) *ErrorResponsePayload {
    return &ErrorResponsePayload{
        Error: message,
        Time:  timeString,
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
    payload := NewErrorResponsePayload(message, time.Now().Format(time.RFC3339))

    response, jsonResponseErr := JsonResponse(statusCode, payload)
    if nil == jsonResponseErr {
        return response
    }

    fallbackPayload := map[string]string{
        "error": message,
        "time":  time.Now().Format(time.RFC3339),
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

func FileResponse(statusCode int, path string) (*Response, error) {
    file, err := os.Open(path)
    if nil != err {
        return nil, err
    }

    headers := make(nethttp.Header)

    extension := filepath.Ext(path)
    if "" != extension {
        contentType := mime.TypeByExtension(extension)
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

func AttachmentResponse(statusCode int, path string, filename string) (*Response, error) {
    response, err := FileResponse(statusCode, path)
    if nil != err {
        return nil, err
    }

    response.headers.Set("Content-Disposition", BuildContentDisposition("attachment", filename))

    return response, nil
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

func RedirectResponse(location string, statusCode int) *Response {
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

func RedirectFound(location string) *Response { return RedirectResponse(location, nethttp.StatusFound) }

func RedirectMovedPermanently(location string) *Response {
    return RedirectResponse(location, nethttp.StatusMovedPermanently)
}
