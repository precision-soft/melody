package outbox

import (
    "context"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    clicontract "github.com/precision-soft/melody/v4/cli/contract"
    "github.com/precision-soft/melody/v4/cli/output"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/logging"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
)

const defaultPollInterval = 1 * time.Second

/* RelayCommandConfig configures the melody:outbox:relay command's run loop. The poll interval, the idle backoff and the batch limit stay the run's flags. */
type RelayCommandConfig struct {
    /* ErrorBackoff is the delay between RunOnce retries after consecutive failures: Initial left out is the run's poll interval, capped at Max, doubled after every failure, capped at 1m, unlimited refused, since a persistent repository outage must neither tight-loop the relay nor push the retry horizon out for good. */
    ErrorBackoff bound.Backoff
}

var relayCommandErrorBackoffRule = bound.BackoffRule{
    Name:           "RelayCommandConfig.ErrorBackoff",
    InitialDefault: bound.TimeoutMinimum,
    MaxDefault:     time.Minute,
    FactorDefault:  2,
}

/* NewRelayCommand builds the command so it resolves service.outbox.relay from the run's scope at the first batch, which lets a process register the command without opening a database at boot; a limit outside its rule is refused. */
func NewRelayCommand(config RelayCommandConfig) *RelayCommand {
    errorBackoff, resolveErr := bound.ResolveBackoff(config.ErrorBackoff, relayCommandErrorBackoffRule)
    if nil != resolveErr {
        exception.Panic(exception.FromError(resolveErr))
    }

    return &RelayCommand{
        errorBackoff:            errorBackoff,
        errorBackoffFromTheRun:  true == config.ErrorBackoff.Initial.IsAbsent(),
    }
}

type RelayCommand struct {
    errorBackoff            bound.ResolvedBackoff
    errorBackoffFromTheRun  bool
}

/* runErrorBackoff answers the error backoff of a run polling at interval: an Initial left out is the interval, capped at Max. */
func (instance *RelayCommand) runErrorBackoff(interval time.Duration) bound.ResolvedBackoff {
    errorBackoff := instance.errorBackoff
    if true == instance.errorBackoffFromTheRun {
        errorBackoff.Initial = min(interval, errorBackoff.Max)
    }

    return errorBackoff
}

/* resolveRelay resolves the run's relay; a failed resolution is reported so the run loop treats it like a failed batch, backs off and retries instead of exiting, and a nil yield is refused so the loop never dereferences it. */
func (instance *RelayCommand) resolveRelay(runtimeInstance containercontract.Runtime) (*Relay, error) {
    relay, resolveErr := runtime.FromRuntime[*Relay](runtimeInstance, ServiceRelay)
    if nil != resolveErr {
        return nil, resolveErr
    }

    if nil == relay {
        return nil, exception.NewError("outbox: the registered relay resolved to nil", nil, nil)
    }

    return relay, nil
}

func (instance *RelayCommand) Name() string {
    return "melody:outbox:relay"
}

func (instance *RelayCommand) Description() string {
    return "drain outbox batches to the message transport until interrupted"
}

func (instance *RelayCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "interval",
            Usage: "poll interval between batches (Go duration, default 1s)",
        },
        &clicontract.StringFlag{
            Name:  "idle-backoff",
            Usage: "sleep when a batch drained nothing (Go duration, defaults to the poll interval)",
        },
        &clicontract.IntFlag{
            Name:   "limit",
            Usage:  "stop after draining this many batches; 0 means run until interrupted",
            Config: output.DecimalIntegerConfig,
        },
    }
}

func (instance *RelayCommand) Run(
    runtimeInstance containercontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    interval, intervalErr := parseDurationFlag(commandContext.String("interval"), defaultPollInterval, "interval")
    if nil != intervalErr {
        return intervalErr
    }

    idleBackoff, idleBackoffErr := parseDurationFlag(commandContext.String("idle-backoff"), interval, "idle-backoff")
    if nil != idleBackoffErr {
        return idleBackoffErr
    }

    limit := commandContext.Int("limit")

    /* stop on SIGINT/SIGTERM: the cancelled context aborts an in-flight batch mid-drain (RunOnce threads it through every repository and transport call), and whatever stayed claimed re-surfaces once its VisibilityTimeout lapses; a signal that lands between batches exits before the next claim. */
    runContext, stop := signal.NotifyContext(runtimeInstance.Context(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    relayRuntime := runtime.WithContext(runtimeInstance, runContext)

    logger := logging.LoggerFromRuntime(relayRuntime)

    batches := 0
    errorBackoff := instance.runErrorBackoff(interval)
    consecutiveFailures := 0
    var relay *Relay

    for {
        /* the relay is resolved inside the loop and kept for the run once resolved: a store outage at start-up surfaces here as an error that follows the same backoff-and-retry path as a failed batch, and a later successful resolution proceeds normally. */
        published := 0
        var runErr error
        if nil == relay {
            relay, runErr = instance.resolveRelay(relayRuntime)
        }

        if nil == runErr {
            published, runErr = relay.RunOnce(relayRuntime)
        }

        batches++

        if nil != runErr {
            if nil != runContext.Err() {
                /* a cancellation interrupted the batch mid-drain and the visibility timeout re-surfaces what stayed claimed, so the command exits cleanly; only an error the cancellation explains is swallowed, so a repository failure that coincides with a lapsing deadline is still reported */
                if true == exception.ChainIs(runErr, context.Canceled) || true == exception.ChainIs(runErr, context.DeadlineExceeded) {
                    return nil
                }

                return runErr
            }

            if 0 < limit && batches >= limit {
                return runErr
            }

            instance.logRunError(logger, runErr, published)

            /* the delay grows over consecutive failures so a persistent outage does not tight-loop; any successful run resets it below */
            consecutiveFailures++
            if false == sleepInterruptible(runContext, errorBackoff.Delay(consecutiveFailures)) {
                return nil
            }

            continue
        }

        consecutiveFailures = 0

        if 0 < limit && batches >= limit {
            return nil
        }

        /* a full drain suggests more rows are due: poll again after the interval; an empty batch waits the idle backoff instead. */
        delay := interval
        if 0 == published {
            delay = idleBackoff
        }

        if false == sleepInterruptible(runContext, delay) {
            return nil
        }
    }
}

func (instance *RelayCommand) logRunError(logger loggingcontract.Logger, runErr error, published int) {
    if nil == logger {
        return
    }

    logger.Error(
        "outbox relay batch failed",
        exception.LogContext(
            runErr,
            exception.LogContextConfig{
                Extra: exceptioncontract.Context{
                    "published": published,
                },
            },
        ),
    )
}

func parseDurationFlag(value string, fallback time.Duration, flagName string) (time.Duration, error) {
    if "" == value {
        return fallback, nil
    }

    parsed, parseErr := time.ParseDuration(value)
    if nil != parseErr {
        return 0, exception.NewError(
            "invalid duration flag",
            exceptioncontract.Context{
                "flag":  flagName,
                "value": value,
            },
            parseErr,
        )
    }

    if 0 >= parsed {
        return 0, exception.NewError(
            "duration flag must be positive",
            exceptioncontract.Context{
                "flag":  flagName,
                "value": value,
            },
            nil,
        )
    }

    return parsed, nil
}

func sleepInterruptible(runContext interface{ Done() <-chan struct{} }, delay time.Duration) bool {
    timer := time.NewTimer(delay)
    defer timer.Stop()

    select {
    case <-runContext.Done():
        return false
    case <-timer.C:
        return true
    }
}

var _ clicontract.Command = (*RelayCommand)(nil)
