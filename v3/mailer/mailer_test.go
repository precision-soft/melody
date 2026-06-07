package mailer_test

import (
    "bufio"
    "bytes"
    "context"
    "encoding/base64"
    "mime"
    "net"
    "net/textproto"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/container"
    "github.com/precision-soft/melody/v3/mailer"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func testRuntime() runtimecontract.Runtime {
    serviceContainer := container.NewContainer()
    return runtime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
}

func TestRenderMessage_MultipartAlternative(t *testing.T) {
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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

    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
        From:    mailercontract.Address{Email: "from@example.com"},
        To:      []mailercontract.Address{{Email: "to@example.com"}},
        Subject: original,
        Text:    "body",
    })
    if nil != renderErr {
        t.Fatalf("render: %v", renderErr)
    }

    /** RFC 5322 §2.1.1: no line may exceed 998 octets excluding the CRLF. A long no-space ASCII subject
        must be chunked into encoded-words rather than emitted as one oversized line. */
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
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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

func TestRenderMessage_FiltersReservedCallerHeaders(t *testing.T) {
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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

    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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

func TestSmtpTransport_RequireAuthFailsWhenServerHasNoAuthExtension(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    transport := mailer.NewSmtpTransport(mailer.SmtpConfig{
        Address:     listener.Addr().String(),
        Username:    "user",
        Password:    "pass",
        RequireAuth: true,
    })

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected RequireAuth to fail when the server does not advertise AUTH")
    }

    if false == strings.Contains(sendErr.Error(), "AUTH") {
        t.Fatalf("expected an AUTH-related error, got %v", sendErr)
    }
}

func TestSmtpTransport_RequireAuthFailsWhenNoUsernameConfigured(t *testing.T) {
    listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
    if nil != listenErr {
        t.Fatalf("listen: %v", listenErr)
    }
    defer listener.Close()

    go serveAuthlessSmtp(listener)

    /** RequireAuth without a username is a misconfiguration (e.g. a secret resolving to ""); the
        transport must fail closed instead of silently delivering the message unauthenticated. */
    transport := mailer.NewSmtpTransport(mailer.SmtpConfig{
        Address:     listener.Addr().String(),
        RequireAuth: true,
    })

    sendErr := transport.Send(testRuntime(), mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "body",
    })
    if nil == sendErr {
        t.Fatalf("expected RequireAuth to fail closed when no username is configured")
    }

    if false == strings.Contains(sendErr.Error(), "username") {
        t.Fatalf("expected a missing-username error, got %v", sendErr)
    }
}

func TestRenderMessage_FoldsLongFirstHeaderToken(t *testing.T) {
    /** A long opening token in a custom header (e.g. a tracking id) must fold onto a continuation
        line; previously the first word never folded and the opening line could breach the limit. */
    longToken := strings.Repeat("A", 995)

    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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

func serveAuthlessSmtp(listener net.Listener) {
    connection, acceptErr := listener.Accept()
    if nil != acceptErr {
        return
    }
    defer connection.Close()

    reader := bufio.NewReader(connection)
    writeLine := func(line string) {
        connection.Write([]byte(line + "\r\n"))
    }

    writeLine("220 fake ESMTP")

    for {
        line, readErr := reader.ReadString('\n')
        if nil != readErr {
            return
        }

        command := strings.ToUpper(strings.TrimSpace(line))
        switch {
        case strings.HasPrefix(command, "EHLO") || strings.HasPrefix(command, "HELO"):
            writeLine("250-fake greets you")
            writeLine("250 SIZE 35882577")
        case strings.HasPrefix(command, "QUIT"):
            writeLine("221 bye")
            return
        default:
            writeLine("250 ok")
        }
    }
}

func TestManager_SendValidatesAndDelegates(t *testing.T) {
    transport := mailer.NewInMemoryTransport()
    manager := mailer.NewManager(transport)
    runtimeInstance := testRuntime()

    missingRecipients := manager.Send(runtimeInstance, mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        Subject: "x",
    })
    if nil == missingRecipients {
        t.Fatalf("expected an error for a message without recipients")
    }

    sendErr := manager.Send(runtimeInstance, mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        To:      []mailercontract.Address{{Email: "ada@example.com"}},
        Subject: "Hello",
        Text:    "Body",
    })
    if nil != sendErr {
        t.Fatalf("send: %v", sendErr)
    }

    sent := transport.Sent()
    if 1 != len(sent) || "Hello" != sent[0].Subject {
        t.Fatalf("expected one recorded message, got %d", len(sent))
    }
}

func TestRenderMessage_FoldsLongAsciiSubject(t *testing.T) {
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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

func TestManager_RejectsMalformedAddress(t *testing.T) {
    transport := mailer.NewInMemoryTransport()
    manager := mailer.NewManager(transport)

    sendErr := manager.Send(testRuntime(), mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "ada@example.com>, attacker@evil.com"}},
        Text: "body",
    })
    if nil == sendErr {
        t.Fatalf("expected a malformed recipient address to be rejected")
    }

    if 0 != len(transport.Sent()) {
        t.Fatalf("expected nothing to be delivered when validation fails")
    }
}

func TestRenderMessage_SkipsEmptyBodyPartWithAttachments(t *testing.T) {
    payload, renderErr := mailer.RenderMessage(mailercontract.Message{
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
