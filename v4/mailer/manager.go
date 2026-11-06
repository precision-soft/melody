package mailer

import (
    "net/mail"

    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal"
    mailercontract "github.com/precision-soft/melody/v4/mailer/contract"
    runtimecontract "github.com/precision-soft/melody/v4/runtime/contract"
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

        parsed, parseErr := mail.ParseAddress(address.Email)
        if nil != parseErr {
            return exception.NewError(
                "mailer message has an invalid address",
                map[string]any{"email": address.Email},
                parseErr,
            )
        }

        /* the renderer and the SMTP envelope write Email as a bare addr-spec, so only one is accepted: the parser's wider grammar takes a display name, angle brackets, a comment or padding, which went out as `RCPT TO:<Bob <bob@x.com>>`; a display name belongs in Name */
        /* a quoted local part (`"bob smith"@example.com`) is unquoted by the parser, so the input is compared with the parsed address re-quoted as the renderer would write it as well as with the parsed address itself */
        if "" != parsed.Name || (parsed.Address != address.Email && (&mail.Address{Address: parsed.Address}).String() != "<"+address.Email+">") {
            return exception.NewError(
                "mailer message address is not a bare addr-spec; put a display name in Name",
                map[string]any{"email": address.Email},
                nil,
            )
        }
    }

    return nil
}

var _ mailercontract.Mailer = (*Manager)(nil)
