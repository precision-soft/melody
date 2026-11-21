package mailer

import (
    "errors"
    "testing"

    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/logging"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

func TestManager_SendValidatesAndDelegates(t *testing.T) {
    transport := NewInMemoryTransport()
    manager := NewManager(transport)
    runtimeInstance := testRuntime()

    _, missingRecipients := manager.Send(runtimeInstance, mailercontract.Message{
        From:    mailercontract.Address{Email: "shop@example.com"},
        Subject: "x",
    })
    if nil == missingRecipients {
        t.Fatalf("expected an error for a message without recipients")
    }

    _, sendErr := manager.Send(runtimeInstance, mailercontract.Message{
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

    _, sendErr := manager.Send(testRuntime(), mailercontract.Message{
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

/* Email is written as a bare addr-spec by the renderer and the SMTP envelope, so only one is accepted: a display name, angle brackets or padding the parser's wider grammar takes is refused, where it went out as `RCPT TO:<Bob <bob@x.com>>` */
func TestManager_RejectsAnEmailThatIsNotABareAddrSpec(t *testing.T) {
    for _, email := range []string{"Bob <bob@example.com>", "<bob@example.com>", " bob@example.com ", "bob@example.com (Bob)"} {
        transport := NewInMemoryTransport()
        manager := NewManager(transport)

        _, sendErr := manager.Send(testRuntime(), mailercontract.Message{
            From: mailercontract.Address{Email: "shop@example.com"},
            To:   []mailercontract.Address{{Email: email}},
            Text: "body",
        })
        if nil == sendErr {
            t.Fatalf("%q: expected an email that is not a bare addr-spec to be refused", email)
        }

        if 0 != len(transport.Sent()) {
            t.Fatalf("%q: expected nothing delivered when validation fails", email)
        }
    }
}

/* an addr-spec with a quoted local part is a bare addr-spec too, and is accepted as before */
func TestManager_AcceptsAQuotedLocalPart(t *testing.T) {
    transport := NewInMemoryTransport()
    manager := NewManager(transport)

    _, sendErr := manager.Send(testRuntime(), mailercontract.Message{
        From: mailercontract.Address{Email: "shop@example.com"},
        To:   []mailercontract.Address{{Email: "\"bob smith\"@example.com"}},
        Text: "body",
    })
    if nil != sendErr {
        t.Fatalf("expected a quoted local part accepted, got %v", sendErr)
    }
}

func TestManager_RejectsRecipientsWithOnlyEmptyEmails(t *testing.T) {
    transport := NewInMemoryTransport()
    manager := NewManager(transport)

    _, sendErr := manager.Send(testRuntime(), mailercontract.Message{
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

type receiptTransport struct {
    receipt mailercontract.Receipt
    sendErr error
}

func (instance *receiptTransport) Send(runtimeInstance containercontract.Runtime, message mailercontract.Message) (mailercontract.Receipt, error) {
    return instance.receipt, instance.sendErr
}

func TestManager_AnswersItsTransportsReceiptAndTheZeroOneBesideAnError(t *testing.T) {
    message := mailercontract.Message{From: mailercontract.Address{Email: "shop@example.com"}, To: []mailercontract.Address{{Email: "ada@example.com"}}}

    receipt, sendErr := NewManager(&receiptTransport{receipt: mailercontract.Receipt{MessageId: "<id@x>"}}).Send(testRuntime(), message)
    if nil != sendErr || "<id@x>" != receipt.MessageId {
        t.Fatalf("expected the transport's receipt, got %+v %v", receipt, sendErr)
    }

    receipt, sendErr = NewManager(&receiptTransport{receipt: mailercontract.Receipt{MessageId: "<half@x>"}, sendErr: errors.New("relay down")}).Send(testRuntime(), message)
    if nil == sendErr || "" != receipt.MessageId {
        t.Fatalf("expected the zero receipt beside the error, got %+v %v", receipt, sendErr)
    }
}

func TestInMemoryAndLogTransports_AnswerTheZeroReceipt(t *testing.T) {
    message := mailercontract.Message{From: mailercontract.Address{Email: "shop@example.com"}, To: []mailercontract.Address{{Email: "ada@example.com"}}}

    for name, transport := range map[string]mailercontract.Transport{
        "in memory": NewInMemoryTransport(),
        "log":       NewLogTransport(logging.NewNopLogger()),
    } {
        receipt, sendErr := transport.Send(testRuntime(), message)
        if nil != sendErr || (mailercontract.Receipt{}) != receipt {
            t.Fatalf("%s: expected the zero receipt, got %+v %v", name, receipt, sendErr)
        }
    }
}
