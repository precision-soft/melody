package cli

import (
    "bytes"
    "testing"

    melodymailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

type recordingMailer struct {
    sent []melodymailercontract.Message
}

func (instance *recordingMailer) Send(runtimeInstance melodyruntimecontract.Runtime, message melodymailercontract.Message) error {
    instance.sent = append(instance.sent, message)

    return nil
}

func TestMailSendCommandReportsTheRecipientOnTheCommandWriter(t *testing.T) {
    mailer := &recordingMailer{}
    captured := &bytes.Buffer{}

    runErr := NewMailSendCommand(mailer).Run(nil, &flagContext{stringByName: map[string]string{"to": "bob@example.com"}, writer: captured})
    if nil != runErr {
        t.Fatalf("expected the email to be sent, got %v", runErr)
    }

    if 1 != len(mailer.sent) {
        t.Fatalf("expected one email handed to the mailer, got %d", len(mailer.sent))
    }

    if "sent email to bob@example.com\n" != captured.String() {
        t.Fatalf("expected the report on the command writer, got %q", captured.String())
    }
}
