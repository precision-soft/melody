package mailer

import (
    "crypto/rand"
    "encoding/base64"
    "encoding/hex"
    "mime"
    "mime/quotedprintable"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

const lineBreak = "\r\n"

/** reservedHeaders are emitted by the renderer itself; a caller-supplied header with the same name
is dropped so it cannot duplicate or override the structural headers. */
var reservedHeaders = map[string]struct{}{
    "from":                      {},
    "to":                        {},
    "cc":                        {},
    "bcc":                       {},
    "reply-to":                  {},
    "subject":                   {},
    "date":                      {},
    "mime-version":              {},
    "content-type":              {},
    "content-transfer-encoding": {},
    "content-disposition":       {},
}

func RenderMessage(message mailercontract.Message) ([]byte, error) {
    if "" == message.From.Email {
        return nil, exception.NewError("mailer message has no sender", nil, nil)
    }

    var builder strings.Builder

    writeHeader(&builder, "From", formatAddress(message.From))

    if 0 < len(message.To) {
        writeHeader(&builder, "To", formatAddressList(message.To))
    }

    if 0 < len(message.Cc) {
        writeHeader(&builder, "Cc", formatAddressList(message.Cc))
    }

    if "" != message.ReplyTo.Email {
        writeHeader(&builder, "Reply-To", formatAddress(message.ReplyTo))
    }

    writeHeader(&builder, "Subject", mime.QEncoding.Encode("utf-8", message.Subject))
    writeHeader(&builder, "Date", time.Now().Format(time.RFC1123Z))
    writeHeader(&builder, "MIME-Version", "1.0")

    for key, value := range message.Headers {
        if _, reserved := reservedHeaders[strings.ToLower(strings.TrimSpace(key))]; true == reserved {
            continue
        }
        writeHeader(&builder, key, value)
    }

    if 0 == len(message.Attachments) {
        writeBodyEntity(&builder, message)

        return []byte(builder.String()), nil
    }

    boundary := newBoundary()
    writeHeader(&builder, "Content-Type", "multipart/mixed; boundary=\""+boundary+"\"")
    builder.WriteString(lineBreak)

    builder.WriteString("--" + boundary + lineBreak)
    writeBodyEntity(&builder, message)

    for _, attachment := range message.Attachments {
        builder.WriteString("--" + boundary + lineBreak)
        writeAttachment(&builder, attachment)
    }

    builder.WriteString("--" + boundary + "--" + lineBreak)

    return []byte(builder.String()), nil
}

/** writeBodyEntity writes the text/html content as a single MIME entity (its own Content-Type plus
encoded body); it is used both at the top level and as the first part of a multipart/mixed message. */
func writeBodyEntity(builder *strings.Builder, message mailercontract.Message) {
    hasHtml := "" != message.Html
    hasText := "" != message.Text

    if true == hasHtml && true == hasText {
        boundary := newBoundary()
        builder.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"" + lineBreak)
        builder.WriteString(lineBreak)
        writeTextPart(builder, boundary, "text/plain; charset=utf-8", message.Text)
        writeTextPart(builder, boundary, "text/html; charset=utf-8", message.Html)
        builder.WriteString("--" + boundary + "--" + lineBreak)

        return
    }

    if true == hasHtml {
        writeTextBody(builder, "text/html; charset=utf-8", message.Html)

        return
    }

    writeTextBody(builder, "text/plain; charset=utf-8", message.Text)
}

func recipients(message mailercontract.Message) []string {
    emails := make([]string, 0, len(message.To)+len(message.Cc)+len(message.Bcc))
    emails = appendEmails(emails, message.To)
    emails = appendEmails(emails, message.Cc)
    emails = appendEmails(emails, message.Bcc)

    return emails
}

func appendEmails(target []string, addresses []mailercontract.Address) []string {
    for _, address := range addresses {
        if "" != address.Email {
            target = append(target, address.Email)
        }
    }

    return target
}

var headerSanitizer = strings.NewReplacer("\r", "", "\n", "")

/** phraseSanitizer drops CR, LF and the quoting characters so an ASCII display name cannot break out
of its quoted-string (a non-ASCII name takes the encoded-word path instead and never needs quotes). */
var phraseSanitizer = strings.NewReplacer("\r", "", "\n", "", "\"", "", "\\", "")

/** filenameSanitizer also drops the double quote so a crafted attachment name cannot break out of the
quoted filename parameter in the Content-Disposition header. */
var filenameSanitizer = strings.NewReplacer("\r", "", "\n", "", "\"", "")

func formatAddress(address mailercontract.Address) string {
    email := headerSanitizer.Replace(address.Email)

    if "" == address.Name {
        return email
    }

    return encodePhrase(address.Name) + " <" + email + ">"
}

/** encodePhrase renders a display name for an address header. A name needing encoding (non-ASCII or
control characters) is emitted as an RFC 2047 encoded-word, which is pure ASCII and must NOT be
quoted; a plain ASCII name keeps the familiar quoted-string form. */
func encodePhrase(name string) string {
    encoded := mime.QEncoding.Encode("utf-8", name)
    if encoded != name {
        return encoded
    }

    return "\"" + phraseSanitizer.Replace(name) + "\""
}

func formatAddressList(addresses []mailercontract.Address) string {
    parts := make([]string, 0, len(addresses))
    for _, address := range addresses {
        if "" == address.Email {
            continue
        }

        parts = append(parts, formatAddress(address))
    }

    return strings.Join(parts, ", ")
}

func writeHeader(builder *strings.Builder, name string, value string) {
    builder.WriteString(headerSanitizer.Replace(name))
    builder.WriteString(": ")
    builder.WriteString(headerSanitizer.Replace(value))
    builder.WriteString(lineBreak)
}

func writeTextPart(builder *strings.Builder, boundary string, contentType string, body string) {
    builder.WriteString("--" + boundary + lineBreak)
    writeTextBody(builder, contentType, body)
}

/** writeTextBody emits a text entity quoted-printable encoded, which keeps every output line within
the SMTP 998-character limit and safely transports 8-bit UTF-8 content. */
func writeTextBody(builder *strings.Builder, contentType string, body string) {
    builder.WriteString("Content-Type: " + contentType + lineBreak)
    builder.WriteString("Content-Transfer-Encoding: quoted-printable" + lineBreak)
    builder.WriteString(lineBreak)
    builder.WriteString(encodeQuotedPrintable(body))
    builder.WriteString(lineBreak)
}

func writeAttachment(builder *strings.Builder, attachment mailercontract.Attachment) {
    contentType := attachment.ContentType
    if "" == contentType {
        contentType = "application/octet-stream"
    }

    builder.WriteString("Content-Type: " + headerSanitizer.Replace(contentType) + lineBreak)
    builder.WriteString("Content-Transfer-Encoding: base64" + lineBreak)
    builder.WriteString("Content-Disposition: attachment; " + filenameParameter(attachment.Filename) + lineBreak)
    builder.WriteString(lineBreak)
    builder.WriteString(encodeBase64Lines(attachment.Content))
    builder.WriteString(lineBreak)
}

func encodeQuotedPrintable(body string) string {
    var encoded strings.Builder

    writer := quotedprintable.NewWriter(&encoded)
    writer.Write([]byte(body))
    writer.Close()

    return encoded.String()
}

/** encodeBase64Lines wraps base64 output at 76 characters per line as required by MIME. */
func encodeBase64Lines(content []byte) string {
    encoded := base64.StdEncoding.EncodeToString(content)

    var wrapped strings.Builder
    for len(encoded) > 76 {
        wrapped.WriteString(encoded[:76])
        wrapped.WriteString(lineBreak)
        encoded = encoded[76:]
    }
    wrapped.WriteString(encoded)

    return wrapped.String()
}

/** filenameParameter renders the Content-Disposition filename. A printable-ASCII name uses the
classic quoted form; anything else is emitted with the RFC 2231 extended syntax (filename*) so
non-ASCII attachment names survive transport instead of being written as raw 8-bit header text. */
func filenameParameter(filename string) string {
    if true == isPrintableAscii(filename) {
        return "filename=\"" + filenameSanitizer.Replace(filename) + "\""
    }

    return "filename*=UTF-8''" + encodeRfc2231(filename)
}

func isPrintableAscii(value string) bool {
    for index := 0; index < len(value); index++ {
        if value[index] < 0x20 || value[index] > 0x7E {
            return false
        }
    }

    return true
}

func encodeRfc2231(value string) string {
    var builder strings.Builder

    for index := 0; index < len(value); index++ {
        character := value[index]
        if true == isAttributeChar(character) {
            builder.WriteByte(character)

            continue
        }

        builder.WriteByte('%')
        builder.WriteString(strings.ToUpper(hex.EncodeToString([]byte{character})))
    }

    return builder.String()
}

/** isAttributeChar reports whether a byte may appear unescaped in an RFC 2231 extended value
(the attr-char set of RFC 5987); everything else is percent-encoded. */
func isAttributeChar(character byte) bool {
    switch {
    case character >= 'A' && character <= 'Z':
        return true
    case character >= 'a' && character <= 'z':
        return true
    case character >= '0' && character <= '9':
        return true
    default:
        return strings.IndexByte("!#$&+-.^_`|~", character) >= 0
    }
}

func newBoundary() string {
    buffer := make([]byte, 16)

    _, readErr := rand.Read(buffer)
    if nil != readErr {
        exception.Panic(exception.NewError("could not generate a mime boundary", nil, readErr))
    }

    return "melody-" + hex.EncodeToString(buffer)
}
