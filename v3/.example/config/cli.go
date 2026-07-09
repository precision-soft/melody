package config

import (
    "time"

    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    "github.com/precision-soft/melody/v3/.example/cli"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodykernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
    melodylock "github.com/precision-soft/melody/v3/lock"
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

    /* @info per-tick dedup demo: with redis configured the demo command is wrapped as an exclusive
       command over the shared locker — run it from two shells at once and exactly one executes, the
       other exits zero with a "skipped" log line. The ttl is crash-safety only; the lock is refreshed
       while the command runs and released the moment it returns. Without redis it runs unwrapped. */
    if nil != instance.redisClient {
        commands = append(
            commands,
            melodylock.NewExclusiveCommand(
                cli.NewExclusiveDemoCommand(),
                melodyrueidis.NewLocker(instance.redisClient),
                30*time.Second,
            ),
        )
    } else {
        commands = append(commands, cli.NewExclusiveDemoCommand())
    }

    return commands
}

var _ melodyapplicationcontract.CliModule = (*Module)(nil)
