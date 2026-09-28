package cli

import (
    "bytes"
    "testing"

    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* sendingBus hands every message to the transport as the routed dispatch bus does, without the handler chain the command does not read */
type sendingBus struct {
    transport melodymessagebuscontract.Transport
}

func (instance *sendingBus) Dispatch(runtimeInstance melodyruntimecontract.Runtime, message any, stamps ...melodymessagebuscontract.Stamp) (melodymessagebuscontract.Envelope, error) {
    envelope := melodymessagebus.NewEnvelope(message, stamps...)

    return envelope, instance.transport.Send(runtimeInstance, envelope)
}

type acceptingBus struct{}

func (instance *acceptingBus) Dispatch(runtimeInstance melodyruntimecontract.Runtime, message any, stamps ...melodymessagebuscontract.Stamp) (melodymessagebuscontract.Envelope, error) {
    return melodymessagebus.NewEnvelope(message, stamps...), nil
}

func TestMessageBusDispatchCommandReportsOnTheCommandWriter(t *testing.T) {
    fixture := newCommandFixture(t)
    transport := melodymessagebus.NewInMemoryTransport(8)
    t.Cleanup(func() { _ = transport.Close() })
    captured := &bytes.Buffer{}

    runErr := NewMessageBusDispatchCommand(&sendingBus{transport: transport}, &acceptingBus{}, transport).Run(fixture.runtime, &flagContext{writer: captured})
    if nil != runErr {
        t.Fatalf("expected the messages to be dispatched and consumed, got %v", runErr)
    }

    expected := "dispatched welcome email for user: 1\n" +
        "dispatched welcome email for user: 2\n" +
        "dispatched welcome email for user: 3\n" +
        "consumed messages: 3\n"
    if expected != captured.String() {
        t.Fatalf("expected the report on the command writer %q, got %q", expected, captured.String())
    }
}
