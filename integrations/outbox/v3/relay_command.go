package outbox

import (
    "context"
    "errors"
    "os"
    "os/signal"
    "syscall"
    "time"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/logging"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    defaultPollInterval = 1 * time.Second

    /* defaultMaxErrorBackoff caps the doubling delay between RunOnce retries after consecutive failures, so a persistent repository outage neither tight-loops the relay nor pushes the retry horizon out indefinitely. */
    defaultMaxErrorBackoff = 1 * time.Minute
)

func NewRelayCommand(relay *Relay) *RelayCommand {
    if nil == relay {
        exception.Panic(exception.NewError("outbox relay command relay is nil", nil, nil))
    }

    return &RelayCommand{relay: relay}
}

/* NewRelayCommandFromResolver builds the command against a lazily-resolved relay: the registered service.outbox.relay is resolved on the first batch rather than in the composition root, so a multi-database app can register the module with a resolver-backed relay factory and still get the built-in command without opening a database at boot. */
func NewRelayCommandFromResolver(resolver containercontract.Resolver) *RelayCommand {
    if nil == resolver {
        exception.Panic(exception.NewError("outbox relay command resolver is nil", nil, nil))
    }

    return &RelayCommand{relayLazy: container.Lazy[*Relay](resolver, ServiceRelay)}
}

type RelayCommand struct {
    relay     *Relay
    relayLazy *container.LazyService[*Relay]
}

/* resolveRelay returns the prebuilt relay, or resolves the lazy one — a successful resolution is memoized, a failed one is reported so the run loop treats it like a failed batch, backs off and retries instead of exiting. The nil-yield guard mirrors lazyRepository.resolveStore: LazyService.Resolve passes a nil yield through with a nil error, and the run loop would then dereference it and panic the relay process on what should have been one more retried failure. */
func (instance *RelayCommand) resolveRelay() (*Relay, error) {
    if nil != instance.relayLazy {
        relay, resolveErr := instance.relayLazy.Resolve()
        if nil != resolveErr {
            return nil, resolveErr
        }

        if nil == relay {
            return nil, exception.NewError("outbox: the registered relay resolved to nil", nil, nil)
        }

        return relay, nil
    }

    return instance.relay, nil
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
            Name:  "limit",
            Usage: "stop after draining this many batches; 0 means run until interrupted",
        },
    }
}

func (instance *RelayCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
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

    relayRuntime := runtime.New(runContext, runtimeInstance.Scope(), runtimeInstance.Container())

    logger := logging.LoggerFromRuntime(relayRuntime)

    batches := 0
    errorBackoff := interval

    for {
        /* the relay is resolved inside the loop: with a lazy relay a store outage at start-up surfaces here as an error that follows the same backoff-and-retry path as a failed batch, and a later successful resolution proceeds normally. */
        published := 0
        relay, runErr := instance.resolveRelay()
        if nil == runErr {
            published, runErr = relay.RunOnce(relayRuntime)
        }

        batches++

        if nil != runErr {
            if nil != runContext.Err() {
                /* a cancellation — signal or parent context — interrupted the batch mid-drain; the visibility timeout re-surfaces whatever stayed claimed, so exit cleanly rather than report the cancellation as a failure. Only an error the cancellation EXPLAINS is swallowed though: a genuine repository failure that merely coincided with a lapsing parent deadline used to ride this branch into exit 0, and a supervisor read the failed drain as success. */
                if true == errors.Is(runErr, context.Canceled) || true == errors.Is(runErr, context.DeadlineExceeded) {
                    return nil
                }

                return runErr
            }

            if 0 < limit && batches >= limit {
                return runErr
            }

            instance.logRunError(logger, runErr, published)

            /* double the delay on consecutive failures so a persistent outage does not tight-loop, clamped to the cap; any successful run resets it below. */
            if false == sleepInterruptible(runContext, errorBackoff) {
                return nil
            }

            errorBackoff = errorBackoff * 2
            if errorBackoff > defaultMaxErrorBackoff || 0 >= errorBackoff {
                errorBackoff = defaultMaxErrorBackoff
            }

            continue
        }

        errorBackoff = interval

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
            exceptioncontract.Context{
                "published": published,
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
