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
)

/* RegisterCliCommands contributes only the application's own commands. The core commands (melody:routes:manifest, melody:openapi:generate, melody:messagebus:consume) are auto-registered by the framework once their services are configured, and the melody:outbox:relay command comes from the outbox module (see configure.go), so they are not listed here. */
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
        /* @info the grant demo holds the user service through a container.Lazy handle built here, at command-registration time: the handle defers the resolution to the command's first run, so this boot-phase composition never races the container. */
        cli.NewGrantDemoCommand(
            melodycontainer.Lazy[*service.UserService](kernelInstance.ServiceContainer(), service.ServiceUserService),
        ),
    }

    /* @info per-tick dedup demo: the demo command is wrapped as an exclusive command over lock.NewLazyLocker, which resolves the registered service.lock.locker (redis when configured, otherwise mysql, otherwise in-memory — see registerLockerService) at the first CreateLock instead of here — run it from two shells at once against a shared locker and exactly one executes, the other exits zero with a "skipped" log line. The ttl is crash-safety only; the lock is refreshed while the command runs and released the moment it returns. */
    commands = append(
        commands,
        melodylock.NewExclusiveCommand(
            cli.NewExclusiveDemoCommand(),
            melodylock.NewLazyLocker(kernelInstance.ServiceContainer()),
            30*time.Second,
        ),
    )

    return commands
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
