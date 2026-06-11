package mailer

import (
    "bufio"
    "bytes"
    "encoding/base64"
    "mime"
    "net/textproto"
    "regexp"
    "strings"
    "testing"
    "unicode/utf8"

    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

func TestRenderMessage_MultipartAlternative(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: "Shop", Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Welcome",
        Text:    "Hello in plain text",
        Html:    "<p>Hello in html</p>",
        Headers: map[string]string{"X-Campaign": "welcome"},
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    for _, expected := range []string{
        "From: \"Shop\" <shop@example.com>",
        "To: ada@example.com",
        "Subject: Welcome",
        "X-Campaign: welcome",
        "Content-Type: multipart/alternative;",
        "text/plain; charset=utf-8",
        "text/html; charset=utf-8",
        "Hello in plain text",
        "<p>Hello in html</p>",
    } {
        if false == strings.Contains(rendered, expected) {
            t.Fatalf("rendered message missing %q\n---\n%s", expected, rendered)
        }
    }
}

func TestRenderMessage_StripsHeaderInjection(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: "Shop\r\nX-Injected: 1", Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello\r\nBcc: victim@example.com",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if true == strings.Contains(rendered, "\nBcc:") {
        t.Fatalf("subject header injection produced a new header line:\n%s", rendered)
    }

    if true == strings.Contains(rendered, "\nX-Injected") {
        t.Fatalf("address name header injection produced a new header line:\n%s", rendered)
    }
}

func TestRenderMessage_EncodesNonAsciiSubjectAndName(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: "Ștefan Mureșan", Email: "stefan@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Comandă confirmată",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if true == strings.Contains(rendered, "Comandă confirmată") {
        t.Fatalf("subject was emitted as raw 8-bit text:\n%s", rendered)
    }

    expectedSubject := "Subject: " + mime.QEncoding.Encode("utf-8", "Comandă confirmată")
    if false == strings.Contains(rendered, expectedSubject) {
        t.Fatalf("expected an encoded-word subject %q in:\n%s", expectedSubject, rendered)
    }

    if true == strings.Contains(rendered, "\"Ștefan Mureșan\"") {
        t.Fatalf("display name was emitted as a raw quoted string:\n%s", rendered)
    }
    if false == strings.Contains(rendered, mime.QEncoding.Encode("utf-8", "Ștefan Mureșan")+" <stefan@example.com>") {
        t.Fatalf("expected an encoded-word display name in:\n%s", rendered)
    }
}

func TestRenderMessage_LongAsciiSubjectStaysUnderHardLineLimit(t *testing.T) {
    original := strings.Repeat("A", 2000)

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "from@example.com"},
        To:      []mailercontract.Address{{Email: "to@example.com"}},
        Subject: original,
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), "\r\n") {
        if 998 < len(line) {
            t.Fatalf("header line exceeds the 998-octet hard limit: %d octets", len(line))
        }
    }

    header, parseErr := textproto.NewReader(bufio.NewReader(bytes.NewReader(payload))).ReadMIMEHeader()
    if nil != parseErr {
        t.Fatalf("parse headers: %v", parseErr)
    }

    decoded, decodeErr := new(mime.WordDecoder).DecodeHeader(header.Get("Subject"))
    if nil != decodeErr {
        t.Fatalf("decode subject: %v", decodeErr)
    }

    if decoded != original {
        t.Fatalf("subject did not round-trip through encoded-word chunking: got %d chars", len(decoded))
    }
}

func TestRenderMessage_EncodesNonAsciiAttachmentFilename(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Text: "see attached",
        Attachments: []mailercontract.Attachment{
            {Filename: "factură.pdf", ContentType: "application/pdf", Content: []byte("pdf")},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if false == strings.Contains(rendered, "filename*=UTF-8''factur%C4%83.pdf") {
        t.Fatalf("expected an RFC 2231 extended filename in:\n%s", rendered)
    }
}

func TestRenderMessage_AttachmentFilenameTrailingBackslashDoesNotEscapeClosingQuote(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Text: "see attached",
        Attachments: []mailercontract.Attachment{
            {Filename: `report\`, ContentType: "application/pdf", Content: []byte("pdf")},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if true == strings.Contains(rendered, `filename="report\"`) {
        t.Fatalf("a trailing backslash escaped the closing quote of the Content-Disposition filename, leaving an unterminated quoted-string:\n%s", rendered)
    }

    if false == strings.Contains(rendered, `filename="report"`) {
        t.Fatalf("expected the backslash to be stripped from the filename (matching the display-name sanitizer), got:\n%s", rendered)
    }
}

func TestRenderMessage_StructuredIdentifierHeadersAreNotEncoded(t *testing.T) {
    longMessageId := "<" + strings.Repeat("a", 70) + "@example.com>"

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
        Headers: map[string]string{
            "In-Reply-To": longMessageId,
            "References":  longMessageId,
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if true == strings.Contains(rendered, "=?utf-8?q?") {
        t.Fatalf("a structured identifier header must not be RFC 2047 encoded (it would break mail threading):\n%s", rendered)
    }

    if false == strings.Contains(rendered, longMessageId) {
        t.Fatalf("expected the In-Reply-To/References message-id to be emitted intact:\n%s", rendered)
    }
}

func TestRenderMessage_FiltersReservedCallerHeaders(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
        Headers: map[string]string{
            "Subject":      "Spoofed",
            "Content-Type": "text/evil",
            "X-Campaign":   "welcome",
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if true == strings.Contains(rendered, "Spoofed") {
        t.Fatalf("expected a reserved Subject header to be dropped:\n%s", rendered)
    }

    if true == strings.Contains(rendered, "text/evil") {
        t.Fatalf("expected a reserved Content-Type header to be dropped:\n%s", rendered)
    }

    if false == strings.Contains(rendered, "X-Campaign: welcome") {
        t.Fatalf("expected a non-reserved custom header to pass through:\n%s", rendered)
    }
}

func TestRenderMessage_AttachmentIsBase64InMixed(t *testing.T) {
    content := []byte("attachment-bytes")

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "With file",
        Text:    "see attached",
        Attachments: []mailercontract.Attachment{
            {Filename: "report\".txt", ContentType: "text/plain", Content: content},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    for _, expected := range []string{
        "Content-Type: multipart/mixed;",
        "Content-Transfer-Encoding: base64",
        "Content-Disposition: attachment; filename=\"report.txt\"",
        base64.StdEncoding.EncodeToString(content),
    } {
        if false == strings.Contains(rendered, expected) {
            t.Fatalf("rendered message missing %q\n---\n%s", expected, rendered)
        }
    }
}

func TestRenderMessage_QuotedPrintableKeepsLinesWithinSmtpLimit(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Long",
        Text:    strings.Repeat("a", 4000),
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), "\r\n") {
        if 998 < len(line) {
            t.Fatalf("rendered line exceeds the SMTP 998-character limit: %d", len(line))
        }
    }
}

func TestRenderMessage_FoldsLongFirstHeaderToken(t *testing.T) {
    longToken := strings.Repeat("A", 995)

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
        Headers: map[string]string{"X-Tracking-Id": longToken + " tail"},
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), "\r\n") {
        if 998 < len(line) {
            t.Fatalf("rendered header line exceeds the RFC 5322 limit: %d", len(line))
        }
    }
}

func TestRenderMessage_FoldsOverlongSpacelessHeaderToken(t *testing.T) {
    spacelessToken := strings.Repeat("A", 999)

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
        Headers: map[string]string{"X-Tracking-Id": spacelessToken},
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), "\r\n") {
        if 998 < len(line) {
            t.Fatalf("rendered header line exceeds the RFC 5322 limit: %d", len(line))
        }
    }
}

func TestRenderMessage_EncodesEspecialsInOverlongAsciiDisplayName(t *testing.T) {
    name := strings.Repeat("a", 30) + ",(b);d" + strings.Repeat("c", 40)

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: name, Email: "from@example.com"},
        To:      []mailercontract.Address{{Email: "to@example.com"}},
        Subject: "Hi",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    header, parseErr := textproto.NewReader(bufio.NewReader(bytes.NewReader(payload))).ReadMIMEHeader()
    if nil != parseErr {
        t.Fatalf("parse headers: %v", parseErr)
    }

    fromHeader := header.Get("From")

    for _, especial := range []string{",", "(", ")", ";"} {
        if true == strings.Contains(fromHeader, especial) {
            t.Fatalf("From header leaked RFC 2047 especial %q into a phrase-context encoded-word: %q", especial, fromHeader)
        }
    }

    if false == strings.Contains(fromHeader, "=2C") {
        t.Fatalf("the comma in the display name was not Q-encoded as =2C: %q", fromHeader)
    }

    decoded, decodeErr := new(mime.WordDecoder).DecodeHeader(fromHeader)
    if nil != decodeErr {
        t.Fatalf("decode From: %v", decodeErr)
    }
    if false == strings.Contains(decoded, name) {
        t.Fatalf("display name did not round-trip through the encoded-words; got %q", decoded)
    }
}

func TestRenderMessage_FoldsLongAsciiSubject(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: strings.TrimSpace(strings.Repeat("word ", 60)),
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    sawFoldedSubject := false
    for _, line := range strings.Split(string(payload), "\r\n") {
        if 998 < len(line) {
            t.Fatalf("rendered header line exceeds the RFC 5322 limit: %d", len(line))
        }
        if true == strings.HasPrefix(line, " word") {
            sawFoldedSubject = true
        }
    }

    if false == sawFoldedSubject {
        t.Fatalf("expected the long subject to be folded onto continuation lines")
    }
}

func TestRenderMessage_SkipsEmptyBodyPartWithAttachments(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Attachments: []mailercontract.Attachment{
            {Filename: "a.txt", Content: []byte("hello")},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)
    if true == strings.Contains(rendered, "text/plain; charset=utf-8") {
        t.Fatalf("expected no empty text/plain body part when the body is empty, got:\n%s", rendered)
    }

    if false == strings.Contains(rendered, "Content-Disposition: attachment") {
        t.Fatalf("expected the attachment part to be present")
    }
}

func TestRenderMessage_LongAsciiDisplayNameStaysUnderHardLineLimit(t *testing.T) {
    name := strings.Repeat("A", 2000)

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: name, Email: "from@example.com"},
        To:      []mailercontract.Address{{Email: "to@example.com"}},
        Subject: "hello",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), "\r\n") {
        if 998 < len(line) {
            t.Fatalf("header line exceeds the 998-octet hard limit: %d octets", len(line))
        }
    }

    header, parseErr := textproto.NewReader(bufio.NewReader(bytes.NewReader(payload))).ReadMIMEHeader()
    if nil != parseErr {
        t.Fatalf("parse headers: %v", parseErr)
    }

    decoded, decodeErr := new(mime.WordDecoder).DecodeHeader(header.Get("From"))
    if nil != decodeErr {
        t.Fatalf("decode from: %v", decodeErr)
    }

    if false == strings.Contains(decoded, name) {
        t.Fatalf("display name did not round-trip through encoded-word chunking")
    }
}

func TestRenderMessage_LongAsciiAttachmentFilenameStaysUnderHardLineLimit(t *testing.T) {
    filename := strings.Repeat("A", 2000) + ".txt"

    payload, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Text: "see attached",
        Attachments: []mailercontract.Attachment{
            {Filename: filename, ContentType: "text/plain", Content: []byte("data")},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), "\r\n") {
        if 998 < len(line) {
            t.Fatalf("rendered line exceeds the 998-octet hard limit: %d octets", len(line))
        }
    }

    if false == strings.Contains(string(payload), "filename*0*=UTF-8''") {
        t.Fatalf("expected RFC 2231 continuation form for an overlong filename")
    }
}

/** @info header folding */

func TestRenderMessage_LongUnfoldableAddressStaysWithinHardLineLimit(t *testing.T) {
    longEmail := "user@" + strings.Repeat("x", 1200) + ".example.com"

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: longEmail},
        To:      []mailercontract.Address{{Email: "recipient@example.com"}},
        Subject: "Test",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("RenderMessage returned an error: %v", renderErr)
    }

    for _, line := range strings.Split(string(payload), lineBreak) {
        if len(line) > maxHardHeaderLineLength {
            t.Fatalf(
                "emitted a %d-octet line, exceeding the RFC 5322 %d-octet hard limit: %.80q...",
                len(line),
                maxHardHeaderLineLength,
                line,
            )
        }
    }
}

func TestFoldHeaderLine_HardWrapPreservesValueBytes(t *testing.T) {
    word := strings.Repeat("a", 3000)
    folded := foldHeaderLine("X-Token", word)

    var rebuilt strings.Builder
    for index, line := range strings.Split(folded, lineBreak) {
        segment := line
        if 0 == index {
            segment = strings.TrimPrefix(line, "X-Token:")
        }
        rebuilt.WriteString(strings.TrimPrefix(segment, " "))

        if len(line) > maxHardHeaderLineLength {
            t.Fatalf("folded line %d is %d octets, exceeding the %d-octet hard limit", index, len(line), maxHardHeaderLineLength)
        }
    }

    if rebuilt.String() != word {
        t.Fatalf("hard-wrapping corrupted the value: rebuilt %d bytes, expected %d", rebuilt.Len(), len(word))
    }
}

/** @info phrase-context encoded-words */

func TestRenderMessage_EncodesEspecialsInNonAsciiDisplayName(t *testing.T) {
    name := "Müller, Inc. (test); <x>"

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: name, Email: "from@example.com"},
        To:      []mailercontract.Address{{Email: "to@example.com"}},
        Subject: "Hi",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    header, parseErr := textproto.NewReader(bufio.NewReader(bytes.NewReader(payload))).ReadMIMEHeader()
    if nil != parseErr {
        t.Fatalf("parse headers: %v", parseErr)
    }

    fromHeader := header.Get("From")

    for _, especial := range []string{",", "(", ")", ";"} {
        if true == strings.Contains(fromHeader, especial) {
            t.Fatalf("non-ASCII From display name leaked RFC 2047 especial %q into a phrase-context encoded-word: %q", especial, fromHeader)
        }
    }

    decoded, decodeErr := new(mime.WordDecoder).DecodeHeader(fromHeader)
    if nil != decodeErr {
        t.Fatalf("decode From: %v", decodeErr)
    }
    if false == strings.Contains(decoded, name) {
        t.Fatalf("display name did not round-trip through the encoded-words; got %q", decoded)
    }
}

func TestRenderMessage_DoesNotSplitMultibyteRuneAcrossEncodedWords(t *testing.T) {
    name := strings.Repeat("a", 57) + "ă" + strings.Repeat("b", 10)

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Name: name, Email: "from@example.com"},
        To:      []mailercontract.Address{{Email: "to@example.com"}},
        Subject: "Hi",
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    header, parseErr := textproto.NewReader(bufio.NewReader(bytes.NewReader(payload))).ReadMIMEHeader()
    if nil != parseErr {
        t.Fatalf("parse headers: %v", parseErr)
    }

    fromHeader := header.Get("From")

    words := regexp.MustCompile(`=\?utf-8\?q\?.*?\?=`).FindAllString(fromHeader, -1)
    if 2 > len(words) {
        t.Fatalf("expected the long non-ASCII display name to chunk into multiple encoded-words, got %d: %q", len(words), fromHeader)
    }

    decoder := new(mime.WordDecoder)
    for _, word := range words {
        decodedWord, decodeErr := decoder.Decode(word)
        if nil != decodeErr {
            t.Fatalf("decode encoded-word %q: %v", word, decodeErr)
        }

        if false == utf8.ValidString(decodedWord) {
            t.Fatalf("encoded-word %q decoded to invalid UTF-8 — a multi-byte rune was split across adjacent encoded-words", word)
        }
    }

    full, decodeErr := decoder.DecodeHeader(fromHeader)
    if nil != decodeErr {
        t.Fatalf("decode From: %v", decodeErr)
    }
    if false == strings.Contains(full, name) {
        t.Fatalf("display name did not round-trip; got %q", full)
    }
}
