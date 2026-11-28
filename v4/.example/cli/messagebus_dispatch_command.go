package cli

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "errors"
    "fmt"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v4/.example/message"
    melodyclicontract "github.com/precision-soft/melody/v4/cli/contract"
    melodyclockcontract "github.com/precision-soft/melody/v4/clock/contract"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    melodymessagebus "github.com/precision-soft/melody/v4/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

const (
    messageBusDispatchFlagTimeout   = "timeout"
    messageBusDispatchFlagNoConsume = "no-consume"

    messageBusDispatchDefaultTimeout = 30 * time.Second
    messageBusDispatchMinimumTimeout = time.Second
    messageBusDispatchRunIdBytes     = 16
)

func NewMessageBusDispatchCommand(
    dispatchBus melodymessagebuscontract.Bus,
    consumeBus melodymessagebuscontract.Bus,
    transport melodymessagebuscontract.Transport,
    clockInstance melodyclockcontract.Clock,
) *MessageBusDispatchCommand {
    return &MessageBusDispatchCommand{
        dispatchBus: dispatchBus,
        consumeBus:  consumeBus,
        transport:   transport,
        clock:       clockInstance,
    }
}

/* MessageBusDispatchCommand dispatches three messages stamped with ids of its run and consumes until those three are handled: beside another consumer of the same queue it may take another producer's messages, which it handles and acks without counting, and it never waits past its timeout. */
type MessageBusDispatchCommand struct {
    dispatchBus melodymessagebuscontract.Bus
    consumeBus  melodymessagebuscontract.Bus
    transport   melodymessagebuscontract.Transport
    clock       melodyclockcontract.Clock
}

func (instance *MessageBusDispatchCommand) Name() string {
    return "messagebus:dispatch"
}

func (instance *MessageBusDispatchCommand) Description() string {
    return "dispatches messages to the async transport and consumes its own in-process, until they are handled or the timeout"
}

func (instance *MessageBusDispatchCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{
        &melodyclicontract.DurationFlag{
            Name:             messageBusDispatchFlagTimeout,
            Usage:            "how long to wait for the run's own messages, a go duration of at least 1s",
            Value:            messageBusDispatchDefaultTimeout,
            Validator:        messageBusDispatchTimeout,
            ValidateDefaults: true,
        },
        &melodyclicontract.BoolFlag{
            Name:  messageBusDispatchFlagNoConsume,
            Usage: "dispatch and return, leaving the messages to the transport's consumers",
        },
    }
}

func (instance *MessageBusDispatchCommand) Run(
    runtimeInstance melodycontainercontract.Runtime,
    commandContext *melodyclicontract.CommandContext,
) error {
    writer := commandContext.Writer

    timeout := commandContext.Duration(messageBusDispatchFlagTimeout)

    runId, runIdErr := newMessageBusDispatchRunId()
    if nil != runIdErr {
        return runIdErr
    }

    messages := []message.WelcomeEmail{
        {UserId: 1, Address: "ada@example.com"},
        {UserId: 2, Address: "alan@example.com"},
        {UserId: 3, Address: "grace@example.com"},
    }

    /* false while the message is still owed, true once it was counted, so a redelivery is not counted twice */
    ownMessageIds := make(map[string]bool, len(messages))

    for index, messageInstance := range messages {
        messageId := runId + "-" + strconv.Itoa(index+1)
        ownMessageIds[messageId] = false

        _, dispatchErr := instance.dispatchBus.Dispatch(
            runtimeInstance,
            messageInstance,
            melodymessagebuscontract.DispatchOptions{
                Stamps: []melodymessagebuscontract.Stamp{melodymessagebus.MessageIdStamp{MessageId: messageId}},
            },
        )
        if nil != dispatchErr {
            return dispatchErr
        }

        _, _ = fmt.Fprintln(writer, "dispatched welcome email for user:", messageInstance.UserId)
    }

    if true == commandContext.Bool(messageBusDispatchFlagNoConsume) {
        return nil
    }

    /* the subscription ends when the command returns, so a delivery it has not taken stays with the queue's other consumers */
    receiveContext, cancelReceive := context.WithCancel(runtimeInstance.Context())
    defer cancelReceive()

    subscription, receiveErr := instance.transport.Receive(melodyruntime.WithContext(runtimeInstance, receiveContext))
    if nil != receiveErr {
        return receiveErr
    }

    timer := instance.clock.NewTimer(timeout)
    defer timer.Stop()

    queue := subscription.Envelopes()
    consumed := 0

    for consumed < len(messages) {
        select {
        case envelopeInstance, open := <-queue:
            if false == open {
                return messageBusDispatchClosedStream(runtimeInstance, subscription, consumed, len(messages))
            }

            _, consumeErr := instance.consumeBus.Dispatch(runtimeInstance, envelopeInstance, melodymessagebuscontract.DispatchOptions{})
            if nil != consumeErr {
                /* no requeue: the queue dead-letters by default, so a message its handler refuses is not redelivered to this loop */
                nackErr := instance.transport.Nack(runtimeInstance, envelopeInstance, melodymessagebuscontract.NackOptions{})

                return errors.Join(consumeErr, nackErr)
            }

            ackErr := instance.transport.Ack(runtimeInstance, envelopeInstance)
            if nil != ackErr {
                return ackErr
            }

            messageId, _ := melodymessagebus.MessageId(envelopeInstance)
            counted, own := ownMessageIds[messageId]
            if false == own {
                _, _ = fmt.Fprintln(writer, "consumed a message of another producer")

                continue
            }

            if true == counted {
                _, _ = fmt.Fprintln(writer, "consumed a redelivery of an own message")

                continue
            }

            ownMessageIds[messageId] = true
            consumed++
        case <-timer.Channel():
            _, _ = fmt.Fprintf(writer, "consumed own messages: %d of %d; the rest were taken by another consumer or are still queued\n", consumed, len(messages))

            return exception.NewError(
                "messagebus:dispatch reached its timeout before its own messages were consumed",
                exceptioncontract.Context{"timeout": timeout.String(), "consumed": consumed, "dispatched": len(messages)},
                nil,
            )
        case <-runtimeInstance.Context().Done():
            return runtimeInstance.Context().Err()
        }
    }

    _, _ = fmt.Fprintf(writer, "consumed own messages: %d of %d\n", consumed, len(messages))

    return nil
}

func messageBusDispatchTimeout(timeout time.Duration) error {
    if timeout < messageBusDispatchMinimumTimeout {
        return exception.NewError(
            "the --timeout flag is below its minimum of 1s",
            exceptioncontract.Context{
                "flag":    messageBusDispatchFlagTimeout,
                "value":   timeout.String(),
                "minimum": messageBusDispatchMinimumTimeout.String(),
            },
            nil,
        )
    }

    return nil
}

func newMessageBusDispatchRunId() (string, error) {
    runIdBytes := make([]byte, messageBusDispatchRunIdBytes)
    if _, readErr := rand.Read(runIdBytes); nil != readErr {
        return "", readErr
    }

    return hex.EncodeToString(runIdBytes), nil
}

func messageBusDispatchClosedStream(
    runtimeInstance melodycontainercontract.Runtime,
    subscription melodymessagebuscontract.Subscription,
    consumed int,
    dispatched int,
) error {
    if streamErr := subscription.Err(); nil != streamErr {
        return streamErr
    }

    if contextErr := runtimeInstance.Context().Err(); nil != contextErr {
        return contextErr
    }

    return exception.NewError(
        "the transport closed its stream before the run's own messages were consumed",
        exceptioncontract.Context{"consumed": consumed, "dispatched": dispatched},
        nil,
    )
}

var _ melodyclicontract.Command = (*MessageBusDispatchCommand)(nil)
