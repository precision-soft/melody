package config

import (
    "context"
    "encoding/json"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    bun "github.com/uptrace/bun"
)

const outboxDemoType = "outbox_demo"

/* the outbox demo wires the transactional-outbox relay through the module's factory shape (see
configure.go): a message enqueued in the same transaction as a business write (atomicity is the whole
point) is later drained to the message transport by the relay, with a stable id so a consumer can
deduplicate the at-least-once delivery. The factories below are registered as the service providers, so
the store and the relay are built from the container at first use — a process that never touches the
outbox never opens its schema or its transport. */

/* outboxStoreFactory is the service.outbox.store provider: it resolves the shared *bun.DB from the
container and ensures the outbox schema at the first resolution, not at boot. The relay publishes each
row to a dedicated amqp queue (or an in-memory transport without AMQP_DSN). */
func (instance *Module) outboxStoreFactory(resolver melodycontainercontract.Resolver) (*outbox.Store, error) {
    database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
    if nil != resolveErr {
        return nil, resolveErr
    }

    store := outbox.NewStore(database, &outboxDemoCodec{})
    if schemaErr := store.EnsureSchema(context.Background()); nil != schemaErr {
        return nil, schemaErr
    }

    return store, nil
}

/* outboxRelayFactory is the service.outbox.relay provider: it resolves the registered store and opens the
transport when the relay is first used — the built-in melody:outbox:relay command resolves its relay the
same way, so an http-mode process registers the command without paying for the amqp connection. */
func (instance *Module) outboxRelayFactory(resolver melodycontainercontract.Resolver) (*outbox.Relay, error) {
    store, resolveErr := melodycontainer.FromResolver[*outbox.Store](resolver, outbox.ServiceStore)
    if nil != resolveErr {
        return nil, resolveErr
    }

    transport, transportErr := instance.buildOutboxTransport()
    if nil != transportErr {
        return nil, transportErr
    }

    return outbox.NewRelay(outbox.RelayConfig{
        Repository: store,
        Transport:  transport,
        Codec:      &outboxDemoCodec{},
        BatchSize:  50,
    }), nil
}

func (instance *Module) buildOutboxTransport() (melodymessagebuscontract.Transport, error) {
    dsn := instance.environmentValue(environmentKeyAmqpDsn)
    if "" == dsn {
        return melodymessagebus.NewInMemoryTransport(64), nil
    }

    provider := amqp.NewProvider()

    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        return nil, openErr
    }

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[message.OutboxDemo](registry, outboxDemoType)

    return amqp.NewTransport(amqp.TransportConfig{
        Connection: connection,
        Dialer:     provider.Dialer(dsn),
        Queue:      "outbox_demo",
        Registry:   registry,
        DeadLetter: true,
    }), nil
}

/* outboxDemoCodec serializes the demo message to and from the outbox row payload. */
type outboxDemoCodec struct{}

func (instance *outboxDemoCodec) Encode(messageInstance any) (string, []byte, error) {
    demo, isDemo := messageInstance.(message.OutboxDemo)
    if false == isDemo {
        return "", nil, exception.NewError("outbox demo codec: unexpected message type", nil, nil)
    }

    payload, marshalErr := json.Marshal(demo)
    if nil != marshalErr {
        return "", nil, marshalErr
    }

    return outboxDemoType, payload, nil
}

func (instance *outboxDemoCodec) Decode(typeName string, payload []byte) (any, error) {
    if outboxDemoType != typeName {
        return nil, exception.NewError("outbox demo codec: unknown type", map[string]any{"type": typeName}, nil)
    }

    var demo message.OutboxDemo
    if unmarshalErr := json.Unmarshal(payload, &demo); nil != unmarshalErr {
        return nil, unmarshalErr
    }

    return demo, nil
}
