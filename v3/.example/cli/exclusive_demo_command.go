package cli

import (
    "fmt"
    "time"

    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* ExclusiveDemoCommand simulates a cron-launched job that must run on exactly one instance per tick: config/cli.go wraps it in lock.NewExclusiveCommand over the Redis locker, so launching it from two shells at once runs the body once while the other exits zero with a "skipped" log line. */
type ExclusiveDemoCommand struct{}

func NewExclusiveDemoCommand() *ExclusiveDemoCommand {
    return &ExclusiveDemoCommand{}
}

func (instance *ExclusiveDemoCommand) Name() string {
    return "example:exclusive:demo"
}

func (instance *ExclusiveDemoCommand) Description() string {
    return "holds the demo lock for --hold and prints the tick; wrapped as an exclusive command when redis is configured"
}

func (instance *ExclusiveDemoCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.StringFlag{
            Name:  "hold",
            Usage: "how long the tick holds the lock (Go duration, default 2s)",
        },
    }
}

func (instance *ExclusiveDemoCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext *melodyclicontract.CommandContext,
) error {
    hold := 2 * time.Second
    if holdFlag := commandContext.String("hold"); "" != holdFlag {
        parsed, parseErr := time.ParseDuration(holdFlag)
        if nil != parseErr {
            return parseErr
        }

        hold = parsed
    }

    fmt.Println("exclusive tick: started, holding the lock for", hold)

    timer := time.NewTimer(hold)
    defer timer.Stop()

    select {
    case <-runtimeInstance.Context().Done():
        fmt.Println("exclusive tick: interrupted")
    case <-timer.C:
        fmt.Println("exclusive tick: done")
    }

    return nil
}

var _ melodyclicontract.Command = (*ExclusiveDemoCommand)(nil)
