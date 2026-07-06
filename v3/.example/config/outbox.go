package config

import (
    "context"
    "encoding/json"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    "github.com/precision-soft/melody/v3/exception"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

const outboxDemoType = "outbox_demo"

/* the outbox demo wires the transactional-outbox relay: a message enqueued in the same transaction as a
business write (atomicity is the whole point) is later drained to the message transport by the relay, with
a stable id so a consumer can deduplicate the at-least-once delivery. Wired only when a database is
configured; the relay publishes to a dedicated amqp queue (or an in-memory transport without AMQP_DSN). */
func (instance *Module) buildOutbox() {
    if nil == instance.database {
        return
    }

    codec := &outboxDemoCodec{}

    store := outbox.NewStore(instance.database, codec)
    if schemaErr := store.EnsureSchema(context.Background()); nil != schemaErr {
        return
    }

    instance.outboxStore = store
    instance.outboxRelay = outbox.NewRelay(outbox.RelayConfig{
        Repository: store,
        Transport:  instance.buildOutboxTransport(),
        Codec:      codec,
        BatchSize:  50,
    })
}

func (instance *Module) buildOutboxTransport() melodymessagebuscontract.Transport {
    dsn := instance.environmentValue(environmentKeyAmqpDsn)
    if "" == dsn {
        return melodymessagebus.NewInMemoryTransport(64)
    }

    provider := amqp.NewProvider()

    connection, openErr := provider.Open(dsn)
    if nil != openErr {
        exception.Panic(exception.FromError(openErr))
    }

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[message.OutboxDemo](registry, outboxDemoType)

    return amqp.NewTransport(amqp.TransportConfig{
        Connection: connection,
        Dialer:     provider.Dialer(dsn),
        Queue:      "outbox_demo",
        Registry:   registry,
        DeadLetter: true,
    })
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
