package cli

import (
    "bytes"
    "testing"

    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodymailercontract "github.com/precision-soft/melody/v4/mailer/contract"
)

type recordingMailer struct {
    sent    []melodymailercontract.Message
    receipt melodymailercontract.Receipt
}

func (instance *recordingMailer) Send(runtimeInstance melodycontainercontract.Runtime, message melodymailercontract.Message) (melodymailercontract.Receipt, error) {
    instance.sent = append(instance.sent, message)

    return instance.receipt, nil
}

func TestMailSendCommandReportsTheRecipientOnTheCommandWriter(t *testing.T) {
    mailer := &recordingMailer{}
    captured := &bytes.Buffer{}

    runErr := NewMailSendCommand(mailer).Run(nil, newStringFlagContext(map[string]string{"to": "bob@example.com"}, captured))
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

func TestMailSendCommandReportsTheReceiptsMessageId(t *testing.T) {
    mailer := &recordingMailer{receipt: melodymailercontract.Receipt{MessageId: "<id@example.com>"}}
    captured := &bytes.Buffer{}

    if runErr := NewMailSendCommand(mailer).Run(nil, newStringFlagContext(map[string]string{"to": "bob@example.com"}, captured)); nil != runErr {
        t.Fatalf("expected the email to be sent, got %v", runErr)
    }

    if "sent email to bob@example.com\nmessage id: <id@example.com>\n" != captured.String() {
        t.Fatalf("expected the receipt's id on the command writer, got %q", captured.String())
    }
}
