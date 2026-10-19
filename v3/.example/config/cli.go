package config

import (
    "os"
    "time"

    "github.com/precision-soft/melody/v3/.example/cli"
    "github.com/precision-soft/melody/v3/.example/service"
    "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

/* RegisterCliCommands contributes only the application's own commands; the core melody commands are auto-registered by the framework once their services are configured, and melody:outbox:relay comes from the outbox module. */
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
        cli.NewInternalSignCommand(instance.internalAuthSigner),
        cli.NewTotpCodeCommand(),
        cli.NewMailSendCommand(instance.mailer),
        cli.NewDatabaseResetCommand(),
        cli.NewCacheClearCommand(),
        /* the grant command holds the user service through a container.Lazy handle, so the resolution waits for the command's first run and this boot-phase composition never races the container. */
        cli.NewGrantRoleCommand(
            melodycontainer.Lazy[*service.UserService](kernelInstance.ServiceContainer(), service.ServiceUserService),
        ),
        /* outside development the directory starts empty, so the first account of a deployment is created here; the password is read from standard input, never from an argument */
        cli.NewUserCreateCommand(
            melodycontainer.Lazy[*service.UserService](kernelInstance.ServiceContainer(), service.ServiceUserService),
            os.Stdin,
        ),
        /* the enrollment door replaces a second factor only for a request carrying a current one, so an account that lost its authenticator and every recovery code is reset by an operator here */
        cli.NewTwoFactorResetCommand(
            melodycontainer.Lazy[*service.UserService](kernelInstance.ServiceContainer(), service.ServiceUserService),
            twofactor.StoreFromRuntime,
        ),
        /* the generator runs inside the application, so every bind is checked against the parameters this configuration declares */
        melodywiring.NewGenerateCommand(NewWiringBindSet()),
    }

    /* the tick command is exclusive over lock.NewLazyLocker, which resolves service.lock.locker at the first CreateLock rather than here; two concurrent runs against a shared locker execute once, the other exits zero with a "skipped" log line. The ttl is crash-safety only: the lock is refreshed while the command runs and released when it returns. */
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
