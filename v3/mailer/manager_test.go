package mailer

import (
    "testing"

    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

func TestManager_SendValidatesAndDelegates(t *testing.T) {
    transport := NewInMemoryTransport()
    manager := NewManager(transport)
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

func TestManager_RejectsMalformedAddress(t *testing.T) {
    transport := NewInMemoryTransport()
    manager := NewManager(transport)

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

func TestManager_RejectsRecipientsWithOnlyEmptyEmails(t *testing.T) {
    transport := NewInMemoryTransport()
    manager := NewManager(transport)

    sendErr := manager.Send(testRuntime(), mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Name: "No Address"}},
        Text: "body",
    })
    if nil == sendErr {
        t.Fatalf("expected a message whose only recipient has an empty email to be rejected")
    }

    if 0 != len(transport.Sent()) {
        t.Fatalf("expected nothing to be delivered when there is no deliverable recipient")
    }
}
