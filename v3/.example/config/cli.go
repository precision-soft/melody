package config

import (
    "github.com/precision-soft/melody/v3/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodyopenapi "github.com/precision-soft/melody/v3/openapi"
)

func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    return []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewProductListCommand(),
        /** @info the cron command is contributed by the cron module (see configure.go). */
        instance.messageBusConsumeCommand,
        cli.NewMessageBusDemoCommand(
            instance.messageBusDispatch,
            instance.messageBusConsume,
            instance.messageBusTransport,
        ),
        cli.NewAuthTokenCommand(instance.jwtSecret),
        melodyopenapi.NewGenerateCommand(instance.openApiInfo, instance.openApiRegistry),
        cli.NewMailSendCommand(instance.mailer),
    }
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
