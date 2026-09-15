package config

import (
    "time"

    "github.com/precision-soft/melody/v3/.example/cli"
    "github.com/precision-soft/melody/v3/.example/service"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

/* RegisterCliCommands contributes only the application's own commands. The core commands (melody:routes:manifest, melody:openapi:generate, melody:messagebus:consume) are auto-registered by the framework once their services are configured, and the melody:outbox:relay command comes from the outbox module (see configure.go), so they are not listed here. */
func (instance *Module) RegisterCliCommands(kernelInstance melodykernelcontract.Kernel) []melodyclicontract.Command {
    commands := []melodyclicontract.Command{
        cli.NewAppInfoCommand(),
        cli.NewCatalogReportRefreshCommand(),
        cli.NewCurrencyRefreshRatesCommand(),
        cli.NewProductListCommand(),
        cli.NewMessageBusDispatchCommand(
            instance.messageBusDispatch,
            instance.messageBusConsume,
            instance.messageBusTransport,
        ),
        cli.NewAuthTokenCommand(instance.jwtSecret),
        cli.NewInternalSignCommand(instance.internalAuthSigner()),
        cli.NewTotpCodeCommand(),
        cli.NewMailSendCommand(instance.mailer),
        cli.NewDatabaseResetCommand(),

        cli.NewGrantRoleCommand(
            melodycontainer.Lazy[*service.UserService](kernelInstance.ServiceContainer(), service.ServiceUserService),
        ),

        melodywiring.NewGenerateCommand(NewWiringBindSet()),
    }

    commands = append(
        commands,
        melodylock.NewExclusiveCommand(
            cli.NewExclusiveTickCommand(),
            melodylock.NewLazyLocker(kernelInstance.ServiceContainer()),
            30*time.Second,
        ),
    )

    return commands
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
