package mailer

import (
    "crypto/rand"
    "encoding/base64"
    "encoding/hex"
    "mime"
    "mime/quotedprintable"
    "strconv"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

const lineBreak = "\r\n"

const maxHeaderLineLength = 78

const maxHardHeaderLineLength = 998

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

    writeHeader(&builder, "Subject", encodeHeaderText(message.Subject))
    writeHeader(&builder, "Date", time.Now().Format(time.RFC1123Z))
    writeHeader(&builder, "MIME-Version", "1.0")

    if false == hasHeader(message.Headers, "message-id") {
        writeHeader(&builder, "Message-ID", newMessageId(message.From.Email))
    }

    for key, value := range message.Headers {
        if _, reserved := reservedHeaders[strings.ToLower(strings.TrimSpace(key))]; true == reserved {
            continue
        }
        writeHeader(&builder, key, encodeHeaderText(value))
    }

    if 0 == len(message.Attachments) {
        writeBodyEntity(&builder, message)

        return []byte(builder.String()), nil
    }

    boundary := newBoundary()
    writeHeader(&builder, "Content-Type", "multipart/mixed; boundary=\""+boundary+"\"")
    builder.WriteString(lineBreak)

    if true == hasBody(message) {
        builder.WriteString("--" + boundary + lineBreak)
        writeBodyEntity(&builder, message)
    }

    for _, attachment := range message.Attachments {
        builder.WriteString("--" + boundary + lineBreak)
        writeAttachment(&builder, attachment)
    }

    builder.WriteString("--" + boundary + "--" + lineBreak)

    return []byte(builder.String()), nil
}

func hasBody(message mailercontract.Message) bool {
    return "" != message.Text || "" != message.Html
}

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

var phraseSanitizer = strings.NewReplacer("\r", "", "\n", "", "\"", "", "\\", "")

var filenameSanitizer = strings.NewReplacer("\r", "", "\n", "", "\"", "")

func formatAddress(address mailercontract.Address) string {
    email := headerSanitizer.Replace(address.Email)

    if "" == address.Name {
        return email
    }

    return encodePhrase(address.Name) + " <" + email + ">"
}

func encodePhrase(name string) string {
    encoded := mime.QEncoding.Encode("utf-8", name)
    if encoded != name {
        return encoded
    }

    if true == hasOverlongToken(name) {
        return encodeWordChunks(name)
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
    builder.WriteString(foldHeaderLine(headerSanitizer.Replace(name), headerSanitizer.Replace(value)))
    builder.WriteString(lineBreak)
}

func hasHeader(headers map[string]string, name string) bool {
    for key := range headers {
        if name == strings.ToLower(strings.TrimSpace(key)) {
            return true
        }
    }

    return false
}

func newMessageId(senderEmail string) string {
    buffer := make([]byte, 16)
    if _, readErr := rand.Read(buffer); nil != readErr {
        exception.Panic(exception.NewError("could not generate a message id", nil, readErr))
    }

    domain := "localhost"
    if at := strings.LastIndexByte(senderEmail, '@'); -1 != at && at+1 < len(senderEmail) {
        domain = senderEmail[at+1:]
    }

    return "<" + hex.EncodeToString(buffer) + "@" + headerSanitizer.Replace(domain) + ">"
}

const maxEncodedWordPayload = 60

func encodeHeaderText(value string) string {
    encoded := mime.QEncoding.Encode("utf-8", value)
    if encoded != value {
        return encoded
    }

    if false == hasOverlongToken(value) {
        return encoded
    }

    return encodeWordChunks(value)
}

func hasOverlongToken(value string) bool {
    for _, token := range strings.Split(value, " ") {
        if len(token) > maxEncodedWordPayload {
            return true
        }
    }

    return false
}

func encodeWordChunks(value string) string {
    var payload strings.Builder
    for index := 0; index < len(value); index++ {
        payload.WriteString(encodeQByte(value[index]))
    }

    remaining := payload.String()

    var builder strings.Builder
    for "" != remaining {
        cut := maxEncodedWordPayload
        if cut > len(remaining) {
            cut = len(remaining)
        }

        for 0 < cut && true == splitsEscapeTriplet(remaining, cut) {
            cut--
        }
        if 0 == cut {
            cut = len(remaining)
        }

        if 0 < builder.Len() {
            builder.WriteString(" ")
        }
        builder.WriteString("=?utf-8?q?")
        builder.WriteString(remaining[:cut])
        builder.WriteString("?=")

        remaining = remaining[cut:]
    }

    return builder.String()
}

func encodeQByte(character byte) string {
    switch {
    case ' ' == character:
        return "_"
    case '=' == character || '?' == character || '_' == character:
        return "=" + strings.ToUpper(hex.EncodeToString([]byte{character}))
    case character > 0x20 && character < 0x7F:
        return string(character)
    default:
        return "=" + strings.ToUpper(hex.EncodeToString([]byte{character}))
    }
}

func splitsEscapeTriplet(payload string, offset int) bool {
    if 1 <= offset && '=' == payload[offset-1] {
        return true
    }

    if 2 <= offset && '=' == payload[offset-2] {
        return true
    }

    return false
}

func foldHeaderLine(name string, value string) string {
    var builder strings.Builder

    builder.WriteString(name)
    builder.WriteString(":")

    lineLength := len(name) + 1

    for _, word := range strings.Split(value, " ") {
        if lineLength+1+len(word) > maxHeaderLineLength {
            builder.WriteString(lineBreak)
            builder.WriteString(" ")
            builder.WriteString(word)
            lineLength = 1 + len(word)

            continue
        }

        builder.WriteString(" ")
        builder.WriteString(word)
        lineLength += 1 + len(word)
    }

    return builder.String()
}

func writeTextPart(builder *strings.Builder, boundary string, contentType string, body string) {
    builder.WriteString("--" + boundary + lineBreak)
    writeTextBody(builder, contentType, body)
}

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
    builder.WriteString(dispositionHeaderLine(attachment.Filename))
    builder.WriteString(lineBreak)
    builder.WriteString(lineBreak)
    builder.WriteString(encodeBase64Lines(attachment.Content))
    builder.WriteString(lineBreak)
}

func dispositionHeaderLine(filename string) string {
    plain := "Content-Disposition: attachment; " + filenameParameter(filename)
    if len(plain) <= maxHardHeaderLineLength {
        return plain
    }

    return foldDispositionFilename(filename)
}

func foldDispositionFilename(filename string) string {
    encoded := encodeRfc2231(filename)

    var builder strings.Builder
    builder.WriteString("Content-Disposition: attachment;")

    segmentIndex := 0
    remaining := encoded
    for {
        prefix := " filename*" + strconv.Itoa(segmentIndex) + "*="
        if 0 == segmentIndex {
            prefix = prefix + "UTF-8''"
        }

        budget := maxHeaderLineLength - len(prefix) - 1
        if budget < 1 {
            budget = 1
        }

        cut := budget
        if cut > len(remaining) {
            cut = len(remaining)
        }

        for 0 < cut && true == splitsPercentTriplet(remaining, cut) {
            cut--
        }
        if 0 == cut {
            cut = len(remaining)
        }

        chunk := remaining[:cut]
        remaining = remaining[cut:]

        builder.WriteString(lineBreak)
        builder.WriteString(prefix)
        builder.WriteString(chunk)
        if "" != remaining {
            builder.WriteString(";")
        }

        segmentIndex = segmentIndex + 1
        if "" == remaining {
            break
        }
    }

    return builder.String()
}

func splitsPercentTriplet(value string, offset int) bool {
    if 1 <= offset && '%' == value[offset-1] {
        return true
    }

    if 2 <= offset && '%' == value[offset-2] {
        return true
    }

    return false
}

func encodeQuotedPrintable(body string) string {
    var encoded strings.Builder

    writer := quotedprintable.NewWriter(&encoded)
    writer.Write([]byte(body))
    writer.Close()

    return encoded.String()
}

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
