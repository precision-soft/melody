package messagebus

import (
    "os"
    "os/signal"
    "syscall"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/logging"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    "github.com/precision-soft/melody/v3/runtime"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewConsumeCommand(
    bus messagebuscontract.Bus,
    transports map[string]messagebuscontract.Transport,
) *ConsumeCommand {
    return &ConsumeCommand{
        bus:        bus,
        transports: transports,
    }
}

type ConsumeCommand struct {
    bus        messagebuscontract.Bus
    transports map[string]messagebuscontract.Transport
}

func (instance *ConsumeCommand) Name() string {
    return "melody:messagebus:consume"
}

func (instance *ConsumeCommand) Description() string {
    return "consume messages from a transport and dispatch them to their handlers"
}

func (instance *ConsumeCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "transport",
            Usage: "name of the registered transport to consume from",
        },
        &clicontract.IntFlag{
            Name:  "limit",
            Usage: "stop after consuming this many messages; 0 means run until interrupted",
        },
    }
}

func (instance *ConsumeCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    transportName := commandContext.String("transport")
    if "" == transportName {
        return exception.NewError("a transport name is required", nil, nil)
    }

    transport, exists := instance.transports[transportName]
    if false == exists {
        return exception.NewError(
            "unknown transport",
            map[string]any{"transport": transportName},
            nil,
        )
    }

    return instance.consumeFrom(runtimeInstance, transport, int64(commandContext.Int("limit")))
}

func (instance *ConsumeCommand) consumeFrom(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    limit int64,
) error {
    consumeContext, stop := signal.NotifyContext(runtimeInstance.Context(), os.Interrupt, syscall.SIGTERM)
    defer stop()

    consumeRuntime := runtime.New(consumeContext, runtimeInstance.Scope(), runtimeInstance.Container())

    queue, receiveErr := transport.Receive(consumeRuntime)
    if nil != receiveErr {
        return receiveErr
    }

    var processed int64

    for {
        select {
        case <-consumeContext.Done():
            return nil
        case envelopeInstance, open := <-queue:
            if false == open {
                return nil
            }

            instance.consume(consumeRuntime, transport, envelopeInstance)

            processed++
            if limit > 0 && processed >= limit {
                return nil
            }
        }
    }
}

func (instance *ConsumeCommand) consume(
    runtimeInstance runtimecontract.Runtime,
    transport messagebuscontract.Transport,
    envelopeInstance messagebuscontract.Envelope,
) {
    _, dispatchErr := instance.bus.Dispatch(runtimeInstance, envelopeInstance)
    if nil != dispatchErr {
        instance.logError(runtimeInstance, "message handling failed, requeueing", dispatchErr)

        nackErr := transport.Nack(runtimeInstance, envelopeInstance, true)
        if nil != nackErr {
            instance.logError(runtimeInstance, "message nack failed", nackErr)
        }

        return
    }

    ackErr := transport.Ack(runtimeInstance, envelopeInstance)
    if nil != ackErr {
        instance.logError(runtimeInstance, "message ack failed", ackErr)
    }
}

func (instance *ConsumeCommand) logError(
    runtimeInstance runtimecontract.Runtime,
    message string,
    err error,
) {
    logger := logging.LoggerFromRuntime(runtimeInstance)
    if nil == logger {
        return
    }

    logger.Error(message, exception.LogContext(err))
}

var _ clicontract.Command = (*ConsumeCommand)(nil)
