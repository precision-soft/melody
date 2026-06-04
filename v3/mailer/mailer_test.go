package mailer_test

import (
    "context"
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
