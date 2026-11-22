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

/* smtpConfig reads the relay's address, its credentials, the plaintext door and the implicit tls switch; a combination the transport would not honour as written refuses the boot naming its keys */
func (instance *Module) smtpConfig() melodymailer.SmtpConfig {
    address := instance.parameterValue(parameterSmtpAddress)
    username := instance.parameterValue(parameterSmtpUsername)
    password := instance.parameterValue(parameterSmtpPassword)
    allowPlaintext := instance.booleanSwitch(environmentKeySmtpAllowPlaintext, parameterSmtpAllowPlaintext)
    implicitTls := instance.booleanSwitch(environmentKeySmtpImplicitTls, parameterSmtpImplicitTls)

    if "" == username && "" != password {
        refuseSmtpConfig("the smtp password is set without a username", map[string]any{"key": environmentKeySmtpPassword, "requires": environmentKeySmtpUsername})
    }

    if "" != username && "" == password {
        refuseSmtpConfig("the smtp username is set without a password", map[string]any{"key": environmentKeySmtpUsername, "requires": environmentKeySmtpPassword})
    }

    /* under implicit tls nothing is ever sent in clear, so the plaintext door would be read as a promise nothing keeps */
    if true == allowPlaintext && true == implicitTls {
        refuseSmtpConfig("the smtp plaintext door is set under implicit tls", map[string]any{"key": environmentKeySmtpAllowPlaintext, "conflictsWith": environmentKeySmtpImplicitTls})
    }

    /* without an address the log transport is wired, so a relay setting would be dropped without a signal */
    if "" == address {
        for _, setting := range []struct{ environmentKey, parameter string }{
            {environmentKeySmtpUsername, parameterSmtpUsername},
            {environmentKeySmtpPassword, parameterSmtpPassword},
            {environmentKeySmtpAllowPlaintext, parameterSmtpAllowPlaintext},
            {environmentKeySmtpImplicitTls, parameterSmtpImplicitTls},
        } {
            if "" != strings.TrimSpace(instance.parameterValue(setting.parameter)) {
                refuseSmtpConfig("the smtp relay setting is set without an address", map[string]any{"key": setting.environmentKey, "requires": environmentKeySmtpAddress})
            }
        }
    }

    return melodymailer.SmtpConfig{
        Address:               address,
        Username:              username,
        Password:              password,
        RequireAuthentication: "" != username,
        AllowPlaintext:        allowPlaintext,
        ImplicitTls:           implicitTls,
    }
}

func refuseSmtpConfig(message string, context map[string]any) {
    melodyexception.Panic(melodyexception.NewError(message, context, nil))
}

/* booleanSwitch reads a key that is empty, "true" or "false", as switchRule admits it; any other value refuses naming the key, since a typo would otherwise switch a protection off with no signal */
func (instance *Module) booleanSwitch(environmentKey string, parameter string) bool {
    value := instance.parameterValue(parameter)

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
