package config

import (
    "context"
    "encoding/json"
    "time"

    amqp "github.com/precision-soft/melody/integrations/amqp/v3"
    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    "github.com/precision-soft/melody/v3/.example/message"
    melodyapplicationcontract "github.com/precision-soft/melody/v3/application/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
    melodymessagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    bun "github.com/uptrace/bun"
)

const outboxNoticeType = "outbox_notice"

/* the transactional outbox: a message enqueued in the same transaction as a business write is drained to the transport by the relay with a stable id, so a consumer can deduplicate the at-least-once delivery. The store and the relay are built from the container at first use. */

/* outboxStoreFactory is the service.outbox.store provider: it resolves the shared *bun.DB from the container and ensures the outbox schema at the first resolution, not at boot. The relay publishes each row to a dedicated amqp queue; without AMQP_DSN it is refused by name and the rows wait. */
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

/* serviceOutboxTransport is the container name of the outbox's own amqp transport, registered as a service so its Close joins the ordered teardown; a connection opened inside a factory is invisible to it. */
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

    /* the application's shared locker — redis when wired, then mysql, then the process's own — so several replicas running melody:outbox:relay from cron drain the outbox from one at a time; the claim alone already keeps two from publishing one row */
    locker, lockerErr := melodycontainer.FromResolver[melodylockcontract.Locker](resolver, melodylock.ServiceLocker)
    if nil != lockerErr {
        return nil, lockerErr
    }

    return outbox.NewRelay(outbox.RelayConfig{
        Repository: store,
        Transport:  transport,
        Codec:      &outboxNoticeCodec{},
        BatchSize:  50,
        Locker:     locker,
        LockName:   outboxRelayLockName,
        LockTtl:    outboxRelayLockTtl,
    }), nil
}

const (
    /* outboxRelayLockName is the lease every replica's relay contends for, named under this application so two applications on one redis never share it */
    outboxRelayLockName = "melody-example-v3:outbox:relay"

    /* outboxRelayLockTtl outlives one batch of fifty publishes; the relay refreshes the lease while it drains */
    outboxRelayLockTtl = 30 * time.Second
)

/* buildOutboxTransport hands the transport ONLY a dialer, no pre-opened connection: the transport closes a connection it dialed itself, while one opened here and handed over would be owned by nobody — the exact leak registering the transport exists to close. The first publish dials; a bad DSN surfaces there through the relay's own backoff-and-retry loop rather than killing the resolution. Without AMQP_DSN no transport is built and the resolution is refused by name, message.ErrTransportNotConfigured: a process-local queue would hold what the relay publishes with nothing in the process reading it, and the rows stay in the outbox until a broker is configured. */
func (instance *Module) buildOutboxTransport() (melodymessagebuscontract.Transport, error) {
    dsn := instance.environmentValue(environmentKeyAmqpDsn)
    if "" == dsn {
        return nil, exception.NewError("the outbox has no transport to publish to", map[string]any{"key": environmentKeyAmqpDsn}, message.ErrTransportNotConfigured)
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
