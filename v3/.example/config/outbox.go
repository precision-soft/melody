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

const serviceOutboxTransport = "service.outbox.transport"

func (instance *Module) registerOutboxTransportService(registrar melodyapplicationcontract.ServiceRegistrar) {
    registrar.RegisterService(
        serviceOutboxTransport,
        func(resolver melodycontainercontract.Resolver) (melodymessagebuscontract.Transport, error) {
            return instance.buildOutboxTransport()
        },
    )
}

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
