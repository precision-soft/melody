package cli

import (
    "fmt"

    "github.com/precision-soft/melody/v3/.example/message"
    melodyclicontract "github.com/precision-soft/melody/v3/cli/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewMessageBusDispatchCommand(
    dispatchBus melodymessagebuscontract.Bus,
    consumeBus melodymessagebuscontract.Bus,
    transport melodymessagebuscontract.Transport,
) *MessageBusDispatchCommand {
    return &MessageBusDispatchCommand{
        dispatchBus: dispatchBus,
        consumeBus:  consumeBus,
        transport:   transport,
    }
}

type MessageBusDispatchCommand struct {
    dispatchBus melodymessagebuscontract.Bus
    consumeBus  melodymessagebuscontract.Bus
    transport   melodymessagebuscontract.Transport
}

func (instance *MessageBusDispatchCommand) Name() string {
    return "messagebus:dispatch"
}

func (instance *MessageBusDispatchCommand) Description() string {
    return "dispatches messages to the async transport and consumes them in-process"
}

func (instance *MessageBusDispatchCommand) Flags() []melodyclicontract.Flag {
    return []melodyclicontract.Flag{}
}

func (instance *MessageBusDispatchCommand) Run(
    runtimeInstance melodyruntimecontract.Runtime,
    commandContext melodyclicontract.Context,
) error {
    messages := []message.WelcomeEmail{
        {UserId: 1, Address: "ada@example.com"},
        {UserId: 2, Address: "alan@example.com"},
        {UserId: 3, Address: "grace@example.com"},
    }

    for _, messageInstance := range messages {
        _, dispatchErr := instance.dispatchBus.Dispatch(runtimeInstance, messageInstance)
        if nil != dispatchErr {
            return dispatchErr
        }

        fmt.Println("dispatched welcome email for user:", messageInstance.UserId)
    }

    queue, receiveErr := instance.transport.Receive(runtimeInstance)
    if nil != receiveErr {
        return receiveErr
    }

    for index := 0; index < len(messages); index++ {
        select {
        case envelopeInstance := <-queue:
            _, consumeErr := instance.consumeBus.Dispatch(runtimeInstance, envelopeInstance)
            if nil != consumeErr {
                return consumeErr
            }

            ackErr := instance.transport.Ack(runtimeInstance, envelopeInstance)
            if nil != ackErr {
                return ackErr
            }
        case <-runtimeInstance.Context().Done():
            return runtimeInstance.Context().Err()
        }
    }

    fmt.Println("consumed messages:", len(messages))

    return nil
}

var _ melodyclicontract.Command = (*MessageBusDispatchCommand)(nil)
