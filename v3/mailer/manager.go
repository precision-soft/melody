package mailer

import (
    "net/mail"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewManager(transport mailercontract.Transport) *Manager {
    if true == internal.IsNilInterface(transport) {
        exception.Panic(exception.NewError("mailer transport is nil", nil, nil))
    }

    return &Manager{
        transport: transport,
    }
}

type Manager struct {
    transport mailercontract.Transport
}

func (instance *Manager) Send(runtimeInstance runtimecontract.Runtime, message mailercontract.Message) error {
    if "" == message.From.Email {
        return exception.NewError("mailer message has no sender", nil, nil)
    }

    if false == hasDeliverableRecipient(message) {
        return exception.NewError("mailer message has no recipients", nil, nil)
    }

    if validationErr := validateAddresses(message); nil != validationErr {
        return validationErr
    }

    return instance.transport.Send(runtimeInstance, message)
}

/** hasDeliverableRecipient reports whether the message has at least one recipient with a non-empty email
    address. Counting slot lengths is not enough: validateAddresses skips entries with an empty email, so a
    recipient slot carrying only a display name would otherwise pass the guard and reach the transport with
    no deliverable recipient (silently accepted by transports that trust the manager's validation). */
func hasDeliverableRecipient(message mailercontract.Message) bool {
    recipients := append([]mailercontract.Address{}, message.To...)
    recipients = append(recipients, message.Cc...)
    recipients = append(recipients, message.Bcc...)

    for _, address := range recipients {
        if "" != address.Email {
            return true
        }
    }

    return false
}

func validateAddresses(message mailercontract.Message) error {
    all := []mailercontract.Address{message.From, message.ReplyTo}
    all = append(all, message.To...)
    all = append(all, message.Cc...)
    all = append(all, message.Bcc...)

    for _, address := range all {
        if "" == address.Email {
            continue
        }

        if _, parseErr := mail.ParseAddress(address.Email); nil != parseErr {
            return exception.NewError(
                "mailer message has an invalid address",
                map[string]any{"email": address.Email},
                parseErr,
            )
        }
    }

    return nil
}

var _ mailercontract.Mailer = (*Manager)(nil)
