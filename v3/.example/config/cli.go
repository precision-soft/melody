package config

import (
    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* RegisterCliCommands contributes only the application's own commands. The core commands (melody:routes:manifest, melody:openapi:generate, melody:messagebus:consume) are auto-registered by the framework once their services are configured, so they are not listed here. */
func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    commands := []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewProductListCommand(),
        cli.NewMessageBusDemoCommand(
            instance.messageBusDispatch,
            instance.messageBusConsume,
            instance.messageBusTransport,
        ),
        cli.NewAuthTokenCommand(instance.jwtSecret),
        cli.NewInternalSignCommand(instance.internalAuthSigner()),
        cli.NewTotpCodeCommand(),
        cli.NewMailSendCommand(instance.mailer),
    }

    /* the outbox relay command is the production way to drain the outbox (a scheduler runs it on an
       interval); it is contributed only when the outbox is wired (a database is configured). */
    if nil != instance.outboxRelay {
        commands = append(commands, outbox.NewRelayCommand(instance.outboxRelay))
    }

    return commands
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
