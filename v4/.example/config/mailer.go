package config

import (
    "strings"

    melodyexception "github.com/precision-soft/melody/v4/exception"
    melodymailer "github.com/precision-soft/melody/v4/mailer"
)

/* without SMTP_ADDRESS the example wires the LogTransport, which writes every send to the request logger instead of delivering it; set SMTP_ADDRESS to deliver through a real server. The nil logger makes the transport resolve the request-scoped logger at send time. */
func (instance *Module) buildMailer() {
    smtpConfig := instance.smtpConfig()
    if "" != smtpConfig.Address {
        instance.mailer = melodymailer.NewManager(
            melodymailer.NewSmtpTransport(smtpConfig),
        )

        return
    }

    instance.mailer = melodymailer.NewManager(
        melodymailer.NewLogTransport(nil),
    )
}

/* smtpConfig reads the relay's address, its credentials and its two tls switches; a combination the transport would not honour as written refuses the boot naming its keys */
func (instance *Module) smtpConfig() melodymailer.SmtpConfig {
    address := instance.environmentValue(environmentKeySmtpAddress)
    username := instance.environmentValue(environmentKeySmtpUsername)
    password := instance.environmentValue(environmentKeySmtpPassword)
    requireTls := instance.booleanSwitch(environmentKeySmtpRequireTls)
    implicitTls := instance.booleanSwitch(environmentKeySmtpImplicitTls)

    if "" == username && "" != password {
        refuseSmtpConfig("the smtp password is set without a username", map[string]any{"key": environmentKeySmtpPassword, "requires": environmentKeySmtpUsername})
    }

    if "" != username && "" == password {
        refuseSmtpConfig("the smtp username is set without a password", map[string]any{"key": environmentKeySmtpUsername, "requires": environmentKeySmtpPassword})
    }

    /* under implicit tls the transport never sends STARTTLS, so the second switch would be read as a promise nothing keeps */
    if true == requireTls && true == implicitTls {
        refuseSmtpConfig("the smtp tls switches are both on", map[string]any{"key": environmentKeySmtpRequireTls, "conflictsWith": environmentKeySmtpImplicitTls})
    }

    /* without an address the log transport is wired, so a relay setting would be dropped without a signal */
    if "" == address {
        for _, environmentKey := range []string{environmentKeySmtpUsername, environmentKeySmtpPassword, environmentKeySmtpRequireTls, environmentKeySmtpImplicitTls} {
            if "" != strings.TrimSpace(instance.environmentValue(environmentKey)) {
                refuseSmtpConfig("the smtp relay setting is set without an address", map[string]any{"key": environmentKey, "requires": environmentKeySmtpAddress})
            }
        }
    }

    return melodymailer.SmtpConfig{
        Address:               address,
        Username:              username,
        Password:              password,
        RequireAuthentication: "" != username,
        RequireTls:            requireTls,
        ImplicitTls:           implicitTls,
    }
}

func refuseSmtpConfig(message string, context map[string]any) {
    melodyexception.Panic(melodyexception.NewError(message, context, nil))
}

/* booleanSwitch reads a key that is empty, "true" or "false"; any other value refuses the boot naming the key, since a typo would otherwise switch a protection off with no signal */
func (instance *Module) booleanSwitch(environmentKey string) bool {
    value := strings.TrimSpace(instance.environmentValue(environmentKey))

    switch value {
    case "", "false":
        return false
    case "true":
        return true
    }

    melodyexception.Panic(melodyexception.NewError(
        "the switch is neither true nor false",
        map[string]any{"key": environmentKey, "value": value},
        nil,
    ))

    return false
}
