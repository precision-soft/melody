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
    "unicode/utf8"

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

/* @important RFC 2047 §5 forbids encoded-words inside the msg-id/addr tokens of these structured headers; Q-encoding them would corrupt the value and silently break mail threading, so they are emitted intact (still CRLF-stripped and folded by writeHeader) rather than routed through encodeHeaderText */
var structuredIdentifierHeaders = map[string]struct{}{
    "message-id":  {},
    "in-reply-to": {},
    "references":  {},
    "content-id":  {},
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
        normalizedKey := strings.ToLower(strings.TrimSpace(key))
        if _, reserved := reservedHeaders[normalizedKey]; true == reserved {
            continue
        }
        if _, structured := structuredIdentifierHeaders[normalizedKey]; true == structured {
            if identifierErr := validateStructuredIdentifierHeader(key, value); nil != identifierErr {
                return nil, identifierErr
            }
            writeHeader(&builder, key, value)
            continue
        }
        writeHeader(&builder, key, encodeHeaderText(value))
    }

    if 0 == len(message.Attachments) {
        writeBodyEntity(&builder, message)

        return []byte(builder.String()), nil
    }

    inlineAttachments, regularAttachments := partitionAttachments(message.Attachments)

    for _, attachment := range inlineAttachments {
        if contentIdErr := validateInlineContentId(attachment.ContentId); nil != contentIdErr {
            return nil, contentIdErr
        }
    }

    if 0 == len(regularAttachments) {
        /* @info only inline attachments: the multipart/related part is the whole message body */
        writeRelatedEntity(&builder, message, inlineAttachments)

        return []byte(builder.String()), nil
    }

    boundary := newBoundary()
    writeHeader(&builder, "Content-Type", "multipart/mixed; boundary=\""+boundary+"\"")
    builder.WriteString(lineBreak)

    if true == hasBody(message) || 0 < len(inlineAttachments) {
        builder.WriteString("--" + boundary + lineBreak)
        if 0 < len(inlineAttachments) {
            writeRelatedEntity(&builder, message, inlineAttachments)
        } else {
            writeBodyEntity(&builder, message)
        }
    }

    for _, attachment := range regularAttachments {
        builder.WriteString("--" + boundary + lineBreak)
        writeAttachment(&builder, attachment)
    }

    builder.WriteString("--" + boundary + "--" + lineBreak)

    return []byte(builder.String()), nil
}

func partitionAttachments(attachments []mailercontract.Attachment) ([]mailercontract.Attachment, []mailercontract.Attachment) {
    inline := make([]mailercontract.Attachment, 0, len(attachments))
    regular := make([]mailercontract.Attachment, 0, len(attachments))

    for _, attachment := range attachments {
        if "" != attachment.ContentId {
            inline = append(inline, attachment)

            continue
        }

        regular = append(regular, attachment)
    }

    return inline, regular
}

/* @info writes a multipart/related entity (RFC 2387) wrapping the message body followed by the inline attachments; the Content-Type line doubles as the message header when this entity is the top-level body, or as the part header when nested inside multipart/mixed. The required type parameter mirrors the media type of the root (first) part so it matches what writeBodyEntity actually emits */
func writeRelatedEntity(builder *strings.Builder, message mailercontract.Message, inlineAttachments []mailercontract.Attachment) {
    boundary := newBoundary()
    builder.WriteString("Content-Type: multipart/related; type=\"" + bodyEntityRootType(message) + "\"; boundary=\"" + boundary + "\"" + lineBreak)
    builder.WriteString(lineBreak)

    builder.WriteString("--" + boundary + lineBreak)
    writeBodyEntity(builder, message)

    for _, attachment := range inlineAttachments {
        builder.WriteString("--" + boundary + lineBreak)
        writeAttachment(builder, attachment)
    }

    builder.WriteString("--" + boundary + "--" + lineBreak)
}

func hasBody(message mailercontract.Message) bool {
    return "" != message.Text || "" != message.Html
}

/* @info the media type of the entity writeBodyEntity emits, used as the multipart/related root type parameter */
func bodyEntityRootType(message mailercontract.Message) string {
    hasHtml := "" != message.Html
    hasText := "" != message.Text

    if true == hasHtml && true == hasText {
        return "multipart/alternative"
    }

    if true == hasHtml {
        return "text/html"
    }

    return "text/plain"
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

var filenameSanitizer = strings.NewReplacer("\r", "", "\n", "", "\"", "", "\\", "")

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
        return encodeWordChunks(name)
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
    var builder strings.Builder
    var word strings.Builder

    flushWord := func() {
        if 0 < builder.Len() {
            builder.WriteString(" ")
        }
        builder.WriteString("=?utf-8?q?")
        builder.WriteString(word.String())
        builder.WriteString("?=")
        word.Reset()
    }

    for _, runeValue := range value {
        token := encodeRune(runeValue)

        if 0 < word.Len() && (word.Len()+len(token)) > maxEncodedWordPayload {
            flushWord()
        }

        word.WriteString(token)
    }

    if 0 < word.Len() {
        flushWord()
    }

    return builder.String()
}

func encodeRune(runeValue rune) string {
    var token strings.Builder
    for _, encodedByte := range []byte(string(runeValue)) {
        token.WriteString(encodeQByte(encodedByte))
    }

    return token.String()
}

func encodeQByte(character byte) string {
    switch {
    case ' ' == character:
        return "_"
    case '=' == character || '?' == character || '_' == character:
        return "=" + strings.ToUpper(hex.EncodeToString([]byte{character}))
    case true == isEncodedWordEspecial(character):
        return "=" + strings.ToUpper(hex.EncodeToString([]byte{character}))
    case character > 0x20 && character < 0x7F:
        return string(character)
    default:
        return "=" + strings.ToUpper(hex.EncodeToString([]byte{character}))
    }
}

func isEncodedWordEspecial(character byte) bool {
    switch character {
    case '(', ')', '<', '>', '@', ',', ';', ':', '"', '/', '[', ']', '.', '\\':
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
            lineLength = 1
        } else {
            builder.WriteString(" ")
            lineLength++
        }

        for lineLength+len(word) > maxHardHeaderLineLength {
            split := runeSafeSplit(word, maxHardHeaderLineLength-lineLength)
            builder.WriteString(word[:split])
            builder.WriteString(lineBreak)
            builder.WriteString(" ")
            word = word[split:]
            lineLength = 1
        }

        builder.WriteString(word)
        lineLength += len(word)
    }

    return builder.String()
}

func runeSafeSplit(value string, limit int) int {
    if limit >= len(value) {
        return len(value)
    }

    if 1 > limit {
        limit = 1
    }

    split := limit
    for split > 0 && false == utf8.RuneStart(value[split]) {
        split--
    }

    if 0 == split {
        return limit
    }

    return split
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

    if "" != attachment.ContentId {
        writeHeader(builder, "Content-ID", bracketContentId(attachment.ContentId))
        if "" != attachment.Filename {
            builder.WriteString(dispositionHeaderLine("inline", attachment.Filename))
        } else {
            builder.WriteString("Content-Disposition: inline")
        }
        builder.WriteString(lineBreak)
    } else {
        builder.WriteString(dispositionHeaderLine("attachment", attachment.Filename))
        builder.WriteString(lineBreak)
    }

    builder.WriteString(lineBreak)
    builder.WriteString(encodeBase64Lines(attachment.Content))
    builder.WriteString(lineBreak)
}

/* @important the Message-ID, In-Reply-To, References and Content-ID headers a caller supplies through the Headers map are sequences of msg-id tokens emitted intact (RFC 2047 §5 forbids Q-encoding them). Two corruption channels are rejected here, mirroring validateInlineContentId for inline Content-IDs. First, any control character: writeHeader strips CR and LF but leaves TAB, DEL and the other C0 bytes, which survive into the value and either invalidate it or get re-read as folding whitespace that splits a token on unfold; the only legitimate whitespace is the single space that separates tokens, so every other whitespace/control rune is refused. Second, length: foldHeaderLine wraps at the spaces between tokens, but a single token longer than a continuation line is hard-split mid-token, injecting whitespace that corrupts the identifier on unfold and silently breaks mail threading, so a token too long to fit on a continuation line (one leading space plus the token) is rejected rather than mangled. */
func validateStructuredIdentifierHeader(name string, value string) error {
    for _, runeValue := range value {
        if '\t' == runeValue || '\r' == runeValue || '\n' == runeValue || runeValue < 0x20 || 0x7F == runeValue {
            return exception.NewError(name+" header contains a control character; a structured identifier header must contain only msg-id tokens separated by single spaces", nil, nil)
        }
    }

    for _, token := range strings.Split(value, " ") {
        if 1+len(token) > maxHardHeaderLineLength {
            return exception.NewError(name+" header has an identifier token too long to encode on a single header line without corrupting it", nil, nil)
        }
    }

    return nil
}

/* @info a Content-ID is a single msg-id token: it may carry no whitespace or control character (either would make the emitted Content-ID header invalid), and folding it would inject whitespace that corrupts the identifier on unfold (RFC 2047 §5 also forbids Q-encoding it), so an id too long to fit on a single 998-octet header line is rejected rather than silently mangled. A continuation line carries one leading space, so the limit is reached when the bracketed value plus that space exceeds maxHardHeaderLineLength */
func validateInlineContentId(contentId string) error {
    for _, runeValue := range contentId {
        if ' ' == runeValue || '\t' == runeValue || '\r' == runeValue || '\n' == runeValue || runeValue < 0x20 || 0x7F == runeValue {
            return exception.NewError("mailer inline attachment Content-ID contains whitespace or a control character; a Content-ID must be a single msg-id token", nil, nil)
        }
    }

    /* @info angle brackets are valid only as a single matched leading-'<'/trailing-'>' pair the caller may already have applied; an interior, unmatched or empty-bracket value would make bracketContentId emit a malformed Content-ID such as <>x<>, so it is rejected rather than wrapped */
    unbracketed := contentId
    if true == strings.HasPrefix(unbracketed, "<") && true == strings.HasSuffix(unbracketed, ">") && 2 <= len(unbracketed) {
        unbracketed = unbracketed[1 : len(unbracketed)-1]
    }
    if "" == unbracketed || true == strings.ContainsAny(unbracketed, "<>") {
        return exception.NewError("mailer inline attachment Content-ID is empty or contains an unmatched or embedded angle bracket; a Content-ID must be a single msg-id token", nil, nil)
    }

    if 1+len(bracketContentId(contentId)) > maxHardHeaderLineLength {
        return exception.NewError("mailer inline attachment Content-ID is too long to encode on a single header line without corrupting the identifier", nil, nil)
    }

    return nil
}

/* @info Content-ID values are msg-id tokens and must be wrapped in angle brackets; a caller-supplied value that already carries them is left untouched */
func bracketContentId(contentId string) string {
    bracketed := contentId

    if false == strings.HasPrefix(bracketed, "<") {
        bracketed = "<" + bracketed
    }

    if false == strings.HasSuffix(bracketed, ">") {
        bracketed = bracketed + ">"
    }

    return bracketed
}

func dispositionHeaderLine(disposition string, filename string) string {
    plain := "Content-Disposition: " + disposition + "; " + filenameParameter(filename)
    if len(plain) <= maxHardHeaderLineLength {
        return plain
    }

    return foldDispositionFilename(disposition, filename)
}

func foldDispositionFilename(disposition string, filename string) string {
    encoded := encodeRfc2231(filename)

    var builder strings.Builder
    builder.WriteString("Content-Disposition: " + disposition + ";")

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
