package mailer

import (
    "crypto/rand"
    "encoding/hex"
    "strings"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

const lineBreak = "\r\n"

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

    writeHeader(&builder, "Subject", message.Subject)
    writeHeader(&builder, "Date", time.Now().Format(time.RFC1123Z))
    writeHeader(&builder, "MIME-Version", "1.0")

    for key, value := range message.Headers {
        writeHeader(&builder, key, value)
    }

    hasHtml := "" != message.Html
    hasText := "" != message.Text

    if true == hasHtml && true == hasText {
        boundary := newBoundary()
        writeHeader(&builder, "Content-Type", "multipart/alternative; boundary=\""+boundary+"\"")
        builder.WriteString(lineBreak)
        writePart(&builder, boundary, "text/plain; charset=utf-8", message.Text)
        writePart(&builder, boundary, "text/html; charset=utf-8", message.Html)
        builder.WriteString("--" + boundary + "--" + lineBreak)

        return []byte(builder.String()), nil
    }

    if true == hasHtml {
        writeHeader(&builder, "Content-Type", "text/html; charset=utf-8")
        builder.WriteString(lineBreak)
        builder.WriteString(message.Html)
        builder.WriteString(lineBreak)

        return []byte(builder.String()), nil
    }

    writeHeader(&builder, "Content-Type", "text/plain; charset=utf-8")
    builder.WriteString(lineBreak)
    builder.WriteString(message.Text)
    builder.WriteString(lineBreak)

    return []byte(builder.String()), nil
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

func formatAddress(address mailercontract.Address) string {
    email := headerSanitizer.Replace(address.Email)

    if "" == address.Name {
        return email
    }

    return "\"" + headerSanitizer.Replace(address.Name) + "\" <" + email + ">"
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

func writePart(builder *strings.Builder, boundary string, contentType string, body string) {
    builder.WriteString("--" + boundary + lineBreak)
    builder.WriteString("Content-Type: " + contentType + lineBreak)
    builder.WriteString(lineBreak)
    builder.WriteString(body)
    builder.WriteString(lineBreak)
}

func newBoundary() string {
    buffer := make([]byte, 16)

    _, readErr := rand.Read(buffer)
    if nil != readErr {
        exception.Panic(exception.NewError("could not generate a mime boundary", nil, readErr))
    }

    return "melody-" + hex.EncodeToString(buffer)
}
