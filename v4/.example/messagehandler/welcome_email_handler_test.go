package messagehandler

import (
    "context"
    "errors"
    "testing"

    "github.com/precision-soft/melody/v4/.example/message"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodymailer "github.com/precision-soft/melody/v4/mailer"
    melodymailercontract "github.com/precision-soft/melody/v4/mailer/contract"
    melodyruntime "github.com/precision-soft/melody/v4/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v4/runtime/contract"
)

type recordingTransport struct {
    sent    []melodymailercontract.Message
    sendErr error
}

func (instance *recordingTransport) Send(_ melodyruntimecontract.Runtime, messageInstance melodymailercontract.Message) error {
    if nil != instance.sendErr {
        return instance.sendErr
    }

    instance.sent = append(instance.sent, messageInstance)

    return nil
}

func runtimeWithMailer(t *testing.T, transport melodymailercontract.Transport) melodyruntimecontract.Runtime {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.Close() })

    if nil != transport {
        mailer := melodymailer.NewManager(transport)
        melodycontainer.MustRegister[melodymailercontract.Mailer](containerInstance, melodymailer.ServiceMailer, func(melodycontainercontract.Resolver) (melodymailercontract.Mailer, error) {
            return mailer, nil
        }, melodycontainer.RegisterOptions{})
    }

    return melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)
}

func TestHandleWelcomeEmail_SendsTheMailThroughTheMailer(t *testing.T) {
    transport := &recordingTransport{}

    if handleErr := HandleWelcomeEmail(runtimeWithMailer(t, transport), message.WelcomeEmail{UserId: 7, Address: "ada@example.com"}); nil != handleErr {
        t.Fatalf("handle: %v", handleErr)
    }

    if 1 != len(transport.sent) || 1 != len(transport.sent[0].To) || "ada@example.com" != transport.sent[0].To[0].Email {
        t.Fatalf("expected one mail to the message's address, got %+v", transport.sent)
    }
}

func TestHandleWelcomeEmail_AFailedSendFailsTheMessage(t *testing.T) {
    sendErr := errors.New("smtp refused")

    if handleErr := HandleWelcomeEmail(runtimeWithMailer(t, &recordingTransport{sendErr: sendErr}), message.WelcomeEmail{UserId: 7, Address: "ada@example.com"}); false == errors.Is(handleErr, sendErr) {
        t.Fatalf("expected the send failure answered, got %v", handleErr)
    }
}

func TestHandleWelcomeEmail_AMissingMailerFailsTheMessageAsAnOutage(t *testing.T) {
    if handleErr := HandleWelcomeEmail(runtimeWithMailer(t, nil), message.WelcomeEmail{UserId: 7, Address: "ada@example.com"}); false == melodyexception.IsInfrastructureFailure(handleErr) {
        t.Fatalf("expected an outage, got %v", handleErr)
    }
}
