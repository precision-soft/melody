package config

import (
    "context"
    "encoding/json"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    bun "github.com/uptrace/bun"
)

const outboxNoticeType = "outbox_notice"

/* the outbox wiring hangs the transactional-outbox relay off the module's factory shape (see configure.go): a message enqueued in the same transaction as a business write (atomicity is the whole point) is later drained to the message transport by the relay, with a stable id so a consumer can deduplicate the at-least-once delivery. The factories below are registered as the service providers, so the store and the relay are built from the container at first use — a process that never touches the outbox never opens its schema or its transport. */

/* outboxStoreFactory is the service.outbox.store provider: it resolves the shared *bun.DB from the container and ensures the outbox schema at the first resolution, not at boot. The relay publishes each row to a dedicated amqp queue (or an in-memory transport without AMQP_DSN). */
func (instance *Module) outboxStoreFactory(resolver melodycontainercontract.Resolver) (*outbox.Store, error) {
    database, resolveErr := melodycontainer.FromResolver[*bun.DB](resolver, serviceDatabase)
    if nil != resolveErr {
        return nil, resolveErr
    }

    store := outbox.NewStore(database, &outboxNoticeCodec{})
    if schemaErr := store.EnsureSchema(context.Background()); nil != schemaErr {
        return nil, schemaErr
    }

    return store, nil
}

/* serviceOutboxTransport is the container name of the outbox's own amqp transport. It is a registered service rather than a private object of the relay factory for exactly the reason the outbox module's GoDoc warns about: a connection opened inside a factory is invisible to container teardown, so the amqp connection lived exactly as long as the process. Registered, the transport's Close() error joins the ordered teardown once the relay has resolved it. */
const serviceOutboxTransport = "service.outbox.transport"

func (instance *Module) registerOutboxTransportService(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        serviceOutboxTransport,
        func(resolver melodycontainercontract.Resolver) (melodymessagebuscontract.Transport, error) {
            return instance.buildOutboxTransport()
        },
    )
}

/* outboxRelayFactory is the service.outbox.relay provider: it resolves the registered store and the registered transport when the relay is first used — the built-in melody:outbox:relay command resolves its relay the same way, so an http-mode process registers the command without paying for the amqp connection. */
func (instance *Module) outboxRelayFactory(resolver melodycontainercontract.Resolver) (*outbox.Relay, error) {
    store, resolveErr := melodycontainer.FromResolver[*outbox.Store](resolver, outbox.ServiceStore)
    if nil != resolveErr {
        return nil, resolveErr
    }

    transport, transportErr := melodycontainer.FromResolver[melodymessagebuscontract.Transport](resolver, serviceOutboxTransport)
    if nil != transportErr {
        return nil, transportErr
    }

    return outbox.NewRelay(outbox.RelayConfig{
        Repository: store,
        Transport:  transport,
        Codec:      &outboxNoticeCodec{},
        BatchSize:  50,
    }), nil
}

/* buildOutboxTransport hands the transport ONLY a dialer, no pre-opened connection: the transport closes a connection it dialed itself, while one opened here and handed over would be owned by nobody — the exact leak registering the transport exists to close. The first publish dials; a bad DSN surfaces there through the relay's own backoff-and-retry loop rather than killing the resolution. */
func (instance *Module) buildOutboxTransport() (melodymessagebuscontract.Transport, error) {
    dsn := instance.environmentValue(environmentKeyAmqpDsn)
    if "" == dsn {
        return melodymessagebus.NewInMemoryTransport(64), nil
    }

    registry := amqp.NewMessageRegistry()
    amqp.RegisterMessage[message.OutboxNotice](registry, outboxNoticeType)

    return amqp.NewTransport(amqp.TransportConfig{
        Dialer:     amqp.NewProvider().Dialer(dsn),
        Queue:      "outbox_notice",
        Registry:   registry,
        DeadLetter: true,
    }), nil
}

/* outboxNoticeCodec serializes the notice to and from the outbox row payload. */
type outboxNoticeCodec struct{}

func (instance *outboxNoticeCodec) Encode(messageInstance any) (string, []byte, error) {
    notice, isNotice := messageInstance.(message.OutboxNotice)
    if false == isNotice {
        return "", nil, exception.NewError("outbox notice codec: unexpected message type", nil, nil)
    }

    payload, marshalErr := json.Marshal(notice)
    if nil != marshalErr {
        return "", nil, marshalErr
    }

    return outboxNoticeType, payload, nil
}

func (instance *outboxNoticeCodec) Decode(typeName string, payload []byte) (any, error) {
    if outboxNoticeType != typeName {
        return nil, exception.NewError("outbox notice codec: unknown type", map[string]any{"type": typeName}, nil)
    }

    var notice message.OutboxNotice
    if unmarshalErr := json.Unmarshal(payload, &notice); nil != unmarshalErr {
        return nil, unmarshalErr
    }

    return notice, nil
}
