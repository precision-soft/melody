package mailer_test

import (
    "context"
    "encoding/base64"
    "mime"
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

    /** The raw UTF-8 must never appear in a header; it has to be carried as an RFC 2047 encoded-word. */
    if true == strings.Contains(rendered, "Comandă confirmată") {
        t.Fatalf("subject was emitted as raw 8-bit text:\n%s", rendered)
    }

    expectedSubject := "Subject: " + mime.QEncoding.Encode("utf-8", "Comandă confirmată")
    if false == strings.Contains(rendered, expectedSubject) {
        t.Fatalf("expected an encoded-word subject %q in:\n%s", expectedSubject, rendered)
    }

    /** A non-ASCII display name must be an unquoted encoded-word, not a quoted raw-UTF-8 string. */
    if true == strings.Contains(rendered, "\"Ștefan Mureșan\"") {
        t.Fatalf("display name was emitted as a raw quoted string:\n%s", rendered)
    }
    if false == strings.Contains(rendered, mime.QEncoding.Encode("utf-8", "Ștefan Mureșan")+" <stefan@example.com>") {
        t.Fatalf("expected an encoded-word display name in:\n%s", rendered)
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
