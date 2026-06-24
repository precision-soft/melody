package mailer

import (
    "bufio"
    "bytes"
    "encoding/base64"
    "io"
    "mime"
    "mime/multipart"
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

func TestRenderMessage_RejectsOverlongStructuredIdentifierHeader(t *testing.T) {
    /* @info a caller-supplied structured-identifier header (In-Reply-To/References/Message-ID/Content-ID) is a sequence of unbreakable msg-id tokens; a single token too long to fit on a header line would be hard-split mid-token by folding, injecting whitespace that corrupts the identifier on unfold, so it is rejected rather than silently mangled (mirrors the inline Content-ID guard) */
    overlongMessageId := "<" + strings.Repeat("a", 1000) + "@example.com>"

    _, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
        Headers: map[string]string{
            "In-Reply-To": overlongMessageId,
        },
    })
    if nil == renderErr {
        t.Fatalf("expected an error for an overlong structured-identifier header, got nil")
    }

    if false == strings.Contains(renderErr.Error(), "In-Reply-To") {
        t.Fatalf("expected the error to name the offending header, got: %v", renderErr)
    }
}

func TestRenderMessage_RejectsControlCharacterInStructuredIdentifierHeader(t *testing.T) {
    /* @info writeHeader strips only CR and LF; a TAB, NUL or other C0 byte a caller embeds in a structured-identifier header would survive into the emitted value and either invalidate it or be re-read as folding whitespace that splits a token on unfold, so it is rejected up front (mirrors the inline Content-ID control-character guard) */
    _, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
        Headers: map[string]string{
            "In-Reply-To": "<a\tb\x00c@example.com>",
        },
    })
    if nil == renderErr {
        t.Fatalf("expected an error for a control character in a structured-identifier header, got nil")
    }

    if false == strings.Contains(renderErr.Error(), "In-Reply-To") {
        t.Fatalf("expected the error to name the offending header, got: %v", renderErr)
    }
}

func TestRenderMessage_AcceptsMultiTokenStructuredIdentifierHeaderWithinLimit(t *testing.T) {
    /* @info a References header carries several msg-id tokens; folding wraps at the spaces between them, so a value whose every individual token fits on a continuation line round-trips intact and every emitted line stays within the 998-octet limit, even when the joined value is far longer */
    first := "<" + strings.Repeat("a", 600) + "@example.com>"
    second := "<" + strings.Repeat("b", 600) + "@example.com>"

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
        Headers: map[string]string{
            "References": first + " " + second,
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if false == strings.Contains(rendered, first) || false == strings.Contains(rendered, second) {
        t.Fatalf("expected both References tokens to be emitted intact:\n%s", rendered)
    }

    for _, line := range strings.Split(rendered, "\r\n") {
        if maxHardHeaderLineLength < len(line) {
            t.Fatalf("rendered References line exceeds the RFC 5322 limit: %d", len(line))
        }
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

func TestRenderMessage_InlineImageUsesMultipartRelatedWithContentId(t *testing.T) {
    logo := []byte("png-bytes")

    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Branded",
        Html:    "<img src=\"cid:logo\">",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: logo, ContentId: "logo"},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    for _, expected := range []string{
        "Content-Type: multipart/related; type=\"text/html\";",
        "Content-ID: <logo>",
        "Content-Disposition: inline",
        "Content-Type: image/png",
        base64.StdEncoding.EncodeToString(logo),
    } {
        if false == strings.Contains(rendered, expected) {
            t.Fatalf("rendered message missing %q\n---\n%s", expected, rendered)
        }
    }

    if true == strings.Contains(rendered, "multipart/mixed") {
        t.Fatalf("an inline-only message must not be wrapped in multipart/mixed\n---\n%s", rendered)
    }

    if true == strings.Contains(rendered, "Content-Disposition: attachment") {
        t.Fatalf("an inline attachment must not use Content-Disposition: attachment\n---\n%s", rendered)
    }
}

func TestRenderMessage_InlineAttachmentCarriesFilenameOnDisposition(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Branded",
        Html:    "<img src=\"cid:logo\">",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: []byte("png"), ContentId: "logo"},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    /* @info a Filename on an inline attachment is preserved as the disposition filename (RFC 2183 allows it on inline) so clients that list inline parts show a name, while the inline disposition and Content-ID stay intact */
    if false == strings.Contains(rendered, "Content-Disposition: inline; filename=\"logo.png\"") {
        t.Fatalf("expected the inline attachment to carry its filename on the disposition\n---\n%s", rendered)
    }

    if false == strings.Contains(rendered, "Content-ID: <logo>") {
        t.Fatalf("expected the inline attachment to keep its Content-ID\n---\n%s", rendered)
    }

    if true == strings.Contains(rendered, "Content-Disposition: attachment") {
        t.Fatalf("an inline attachment must not use Content-Disposition: attachment\n---\n%s", rendered)
    }
}

func TestRenderMessage_InlineAttachmentWithoutFilenameKeepsBareInlineDisposition(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Branded",
        Html:    "<img src=\"cid:logo\">",
        Attachments: []mailercontract.Attachment{
            {ContentType: "image/png", Content: []byte("png"), ContentId: "logo"},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if false == strings.Contains(rendered, "Content-Disposition: inline"+lineBreak) {
        t.Fatalf("expected a bare inline disposition when the inline attachment has no filename\n---\n%s", rendered)
    }

    if true == strings.Contains(rendered, "Content-Disposition: inline; filename") {
        t.Fatalf("an inline attachment without a filename must not emit a filename parameter\n---\n%s", rendered)
    }
}

func TestRenderMessage_InlineImageRelatedTypeMatchesAlternativeRoot(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Branded",
        Text:    "plain fallback",
        Html:    "<img src=\"cid:logo\"> hello",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: []byte("png"), ContentId: "logo"},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    /* @info with both Text and Html the related root is a multipart/alternative, so the related type parameter must say so (RFC 2387) */
    if false == strings.Contains(rendered, "multipart/related; type=\"multipart/alternative\";") {
        t.Fatalf("expected related type to match the multipart/alternative root\n---\n%s", rendered)
    }

    if true == strings.Contains(rendered, "type=\"text/html\"") {
        t.Fatalf("related type must not claim text/html when the root is multipart/alternative\n---\n%s", rendered)
    }
}

func TestRenderMessage_ContentIdAlreadyBracketedIsNotDoubleWrapped(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Html: "<img src=\"cid:logo@host\">",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: []byte("x"), ContentId: "<logo@host>"},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if false == strings.Contains(rendered, "Content-ID: <logo@host>") {
        t.Fatalf("expected a single set of angle brackets around the content id\n---\n%s", rendered)
    }

    if true == strings.Contains(rendered, "<<") || true == strings.Contains(rendered, ">>") {
        t.Fatalf("content id must not be double-wrapped\n---\n%s", rendered)
    }
}

func TestRenderMessage_InlineAndRegularAttachmentNestRelatedInsideMixed(t *testing.T) {
    payload, renderErr := RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Invoice",
        Html:    "<img src=\"cid:logo\"> see attached",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: []byte("png"), ContentId: "logo"},
            {Filename: "invoice.pdf", ContentType: "application/pdf", Content: []byte("pdf")},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    topHeader, bodyReader := splitMimeHeaderAndBody(t, payload)

    topMediaType, topParams, parseErr := mime.ParseMediaType(topHeader.Get("Content-Type"))
    if nil != parseErr {
        t.Fatalf("parse top content-type: %v", parseErr)
    }
    if "multipart/mixed" != topMediaType {
        t.Fatalf("expected top-level multipart/mixed, got %q", topMediaType)
    }

    mixedReader := multipart.NewReader(bodyReader, topParams["boundary"])

    relatedPart, relatedErr := mixedReader.NextPart()
    if nil != relatedErr {
        t.Fatalf("read first mixed part: %v", relatedErr)
    }
    relatedMediaType, relatedParams, relatedParseErr := mime.ParseMediaType(relatedPart.Header.Get("Content-Type"))
    if nil != relatedParseErr {
        t.Fatalf("parse first part content-type: %v", relatedParseErr)
    }
    if "multipart/related" != relatedMediaType {
        t.Fatalf("expected first mixed part to be multipart/related, got %q", relatedMediaType)
    }

    relatedReader := multipart.NewReader(relatedPart, relatedParams["boundary"])

    bodyPart, bodyPartErr := relatedReader.NextPart()
    if nil != bodyPartErr {
        t.Fatalf("read related body part: %v", bodyPartErr)
    }
    if false == strings.HasPrefix(bodyPart.Header.Get("Content-Type"), "text/html") {
        t.Fatalf("expected related body part to be text/html, got %q", bodyPart.Header.Get("Content-Type"))
    }

    imagePart, imagePartErr := relatedReader.NextPart()
    if nil != imagePartErr {
        t.Fatalf("read related image part: %v", imagePartErr)
    }
    if "<logo>" != imagePart.Header.Get("Content-Id") {
        t.Fatalf("expected inline image Content-ID <logo>, got %q", imagePart.Header.Get("Content-Id"))
    }

    pdfPart, pdfPartErr := mixedReader.NextPart()
    if nil != pdfPartErr {
        t.Fatalf("read second mixed part: %v", pdfPartErr)
    }
    if false == strings.HasPrefix(pdfPart.Header.Get("Content-Disposition"), "attachment") {
        t.Fatalf("expected the regular attachment to keep Content-Disposition: attachment, got %q", pdfPart.Header.Get("Content-Disposition"))
    }
}

func splitMimeHeaderAndBody(t *testing.T, payload []byte) (textproto.MIMEHeader, *bytes.Reader) {
    t.Helper()

    reader := bufio.NewReader(bytes.NewReader(payload))
    header, parseErr := textproto.NewReader(reader).ReadMIMEHeader()
    if nil != parseErr {
        t.Fatalf("parse mime header: %v", parseErr)
    }

    remaining, readErr := io.ReadAll(reader)
    if nil != readErr {
        t.Fatalf("read mime body: %v", readErr)
    }

    return header, bytes.NewReader(remaining)
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

func TestRenderMessage_RejectsOverlongInlineContentId(t *testing.T) {
    /* @info a Content-ID is an unbreakable msg-id token; folding it would inject whitespace and corrupt the identifier on unfold, so an id too long to fit on a single header line is rejected rather than silently mangled */
    overlongContentId := strings.Repeat("a", 1200)

    _, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Html: "<img src=\"cid:logo\">",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: []byte("png"), ContentId: overlongContentId},
        },
    })
    if nil == renderErr {
        t.Fatalf("expected an error for an overlong inline Content-ID, got nil")
    }

    if false == strings.Contains(renderErr.Error(), "Content-ID") {
        t.Fatalf("expected the error to mention the Content-ID, got: %v", renderErr)
    }
}

func TestRenderMessage_RejectsInlineContentIdWithWhitespace(t *testing.T) {
    /* @info a Content-ID is a single msg-id token; an embedded space would make the emitted Content-ID header invalid, so it is rejected rather than passed through */
    _, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Html: "<img src=\"cid:logo\">",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: []byte("png"), ContentId: "bad id"},
        },
    })
    if nil == renderErr {
        t.Fatalf("expected an error for a Content-ID containing whitespace, got nil")
    }

    if false == strings.Contains(renderErr.Error(), "Content-ID") {
        t.Fatalf("expected the error to mention the Content-ID, got: %v", renderErr)
    }
}

func TestRenderMessage_RejectsInlineContentIdWithUnmatchedAngleBracket(t *testing.T) {
    /* @info bracketContentId wraps a bare id and leaves an already-matched <...> pair untouched; an interior or unmatched angle bracket such as >x< would otherwise be emitted as a malformed Content-ID like <>x<>, so it is rejected rather than wrapped */
    for _, contentId := range []string{">x<", "a<b>c", "<a><b>", "<>"} {
        _, renderErr := RenderMessage(mailercontract.Message{
            From: mailercontract.Address{Email: "shop@example.com"},
            To:   []mailercontract.Address{{Email: "ada@example.com"}},
            Html: "<img src=\"cid:logo\">",
            Attachments: []mailercontract.Attachment{
                {Filename: "logo.png", ContentType: "image/png", Content: []byte("png"), ContentId: contentId},
            },
        })
        if nil == renderErr {
            t.Fatalf("expected an error for a malformed Content-ID %q, got nil", contentId)
        }

        if false == strings.Contains(renderErr.Error(), "Content-ID") {
            t.Fatalf("expected the error to mention the Content-ID for %q, got: %v", contentId, renderErr)
        }
    }
}

func TestRenderMessage_AcceptsLongButFoldableInlineContentId(t *testing.T) {
    /* @info a long-but-not-overlong id soft-folds onto a continuation line without a hard split, so the value round-trips intact and every line stays within the 998-octet limit */
    longContentId := strings.Repeat("a", 600)

    payload, renderErr := RenderMessage(mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com"}},
        Html: "<img src=\"cid:logo\">",
        Attachments: []mailercontract.Attachment{
            {Filename: "logo.png", ContentType: "image/png", Content: []byte("png"), ContentId: longContentId},
        },
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    rendered := string(payload)

    if false == strings.Contains(rendered, "Content-Disposition: inline") {
        t.Fatalf("expected the attachment to render as inline\n---\n%s", rendered)
    }

    for _, line := range strings.Split(rendered, "\r\n") {
        if 998 < len(line) {
            t.Fatalf("rendered Content-ID line exceeds the RFC 5322 limit: %d", len(line))
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

/* @info header folding */

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

/* @info phrase-context encoded-words */

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
