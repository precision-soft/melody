package rueidis

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "sync"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    loggingcontract "github.com/precision-soft/melody/v3/logging/contract"
    "github.com/redis/rueidis"
)

const defaultServerSentEventBackplaneChannel = "melody:sse"

type serverSentEventWireEvent struct {
    Origin string              `json:"origin"`
    Topic  string              `json:"topic"`
    Event  melodyhttp.ServerSentEvent `json:"event"`
}

type ServerSentEventBackplane struct {
    client    rueidis.Client
    hub       *melodyhttp.ServerSentEventHub
    channel   string
    origin    string
    logger    loggingcontract.Logger
    reconnect ReconnectConfig

    ctx    context.Context
    cancel context.CancelFunc
    wait   sync.WaitGroup
}

type ServerSentEventBackplaneOption func(*ServerSentEventBackplane)

func WithServerSentEventBackplaneChannel(channel string) ServerSentEventBackplaneOption {
    return func(backplane *ServerSentEventBackplane) {
        backplane.channel = channel
    }
}

func WithServerSentEventBackplaneLogger(logger loggingcontract.Logger) ServerSentEventBackplaneOption {
    return func(backplane *ServerSentEventBackplane) {
        backplane.logger = logger
    }
}

func WithServerSentEventBackplaneReconnectConfig(reconnectConfig *ReconnectConfig) ServerSentEventBackplaneOption {
    return func(backplane *ServerSentEventBackplane) {
        backplane.reconnect = resolveReconnectConfig(reconnectConfig)
    }
}

func NewServerSentEventBackplane(client rueidis.Client, hub *melodyhttp.ServerSentEventHub, options ...ServerSentEventBackplaneOption) *ServerSentEventBackplane {
    if nil == client {
        exception.Panic(exception.NewError("redis sse backplane client is nil", nil, nil))
    }

    if nil == hub {
        exception.Panic(exception.NewError("redis sse backplane hub is nil", nil, nil))
    }

    ctx, cancel := context.WithCancel(context.Background())

    backplane := &ServerSentEventBackplane{
        client:    client,
        hub:       hub,
        channel:   defaultServerSentEventBackplaneChannel,
        origin:    newBackplaneOrigin(),
        reconnect: resolveReconnectConfig(nil),
        ctx:       ctx,
        cancel:    cancel,
    }

    for _, option := range options {
        option(backplane)
    }

    if "" == backplane.channel {
        backplane.channel = defaultServerSentEventBackplaneChannel
    }

    hub.SetBackplane(backplane)

    backplane.wait.Add(1)
    go backplane.listen()

    return backplane
}

func (instance *ServerSentEventBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    payload, marshalErr := json.Marshal(serverSentEventWireEvent{Origin: instance.origin, Topic: topic, Event: event})
    if nil != marshalErr {
        return exception.NewError("redis sse backplane could not encode the event", map[string]any{"topic": topic}, marshalErr)
    }

    result := instance.client.Do(
        instance.ctx,
        instance.client.B().Publish().Channel(instance.channel).Message(string(payload)).Build(),
    )
    if resultErr := result.Error(); nil != resultErr {
        return exception.NewError("redis sse backplane publish failed", map[string]any{"topic": topic}, resultErr)
    }

    return nil
}

func (instance *ServerSentEventBackplane) Close() error {
    instance.hub.SetBackplane(nil)
    instance.cancel()
    instance.wait.Wait()

    return nil
}

func (instance *ServerSentEventBackplane) listen() {
    defer instance.wait.Done()

    backoff := instance.reconnect.InitialBackoff

    for {
        if nil != instance.ctx.Err() {
            return
        }

        startedAt := time.Now()

        receiveErr := instance.client.Receive(
            instance.ctx,
            instance.client.B().Subscribe().Channel(instance.channel).Build(),
            instance.handle,
        )
        if nil != instance.ctx.Err() {
            return
        }

        if nil != receiveErr {
            instance.logError("redis sse backplane subscription lost, resubscribing", receiveErr)
        }

        if true == instance.shouldResetBackplaneBackoff(time.Since(startedAt)) {
            backoff = instance.reconnect.InitialBackoff

            continue
        }

        select {
        case <-time.After(backoff):
        case <-instance.ctx.Done():
            return
        }

        backoff = instance.nextServerSentEventBackplaneBackoff(backoff)
    }
}

func (instance *ServerSentEventBackplane) shouldResetBackplaneBackoff(subscriptionDuration time.Duration) bool {
    return instance.reconnect.InitialBackoff <= subscriptionDuration
}

func (instance *ServerSentEventBackplane) handle(message rueidis.PubSubMessage) {
    wire := serverSentEventWireEvent{}
    if unmarshalErr := json.Unmarshal([]byte(message.Message), &wire); nil != unmarshalErr {
        instance.logError("redis sse backplane could not decode an event", unmarshalErr)

        return
    }

    if wire.Origin == instance.origin {
        return
    }

    instance.hub.DeliverLocal(wire.Topic, wire.Event)
}

func (instance *ServerSentEventBackplane) logError(message string, err error) {
    if nil == instance.logger {
        return
    }

    instance.logger.Error(message, exception.LogContext(err))
}

func (instance *ServerSentEventBackplane) nextServerSentEventBackplaneBackoff(current time.Duration) time.Duration {
    next := time.Duration(float64(current) * instance.reconnect.BackoffFactor)
    if next > instance.reconnect.MaxBackoff {
        return instance.reconnect.MaxBackoff
    }

    return next
}

func newBackplaneOrigin() string {
    buffer := make([]byte, 16)

    if _, readErr := rand.Read(buffer); nil != readErr {
        exception.Panic(exception.NewError("could not generate a backplane origin", nil, readErr))
    }

    return hex.EncodeToString(buffer)
}

var _ melodyhttp.ServerSentEventBackplane = (*ServerSentEventBackplane)(nil)
