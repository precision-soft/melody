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

const (
    defaultSseBackplaneChannel = "melody:sse"

    sseBackplaneInitialBackoff = 1 * time.Second
    sseBackplaneMaxBackoff     = 30 * time.Second
    sseBackplaneBackoffFactor  = 2
)

type sseWireEvent struct {
    Origin string              `json:"origin"`
    Topic  string              `json:"topic"`
    Event  melodyhttp.SseEvent `json:"event"`
}

type SseBackplane struct {
    client  rueidis.Client
    hub     *melodyhttp.SseHub
    channel string
    origin  string
    logger  loggingcontract.Logger

    ctx    context.Context
    cancel context.CancelFunc
    wait   sync.WaitGroup
}

type SseBackplaneOption func(*SseBackplane)

func WithSseBackplaneChannel(channel string) SseBackplaneOption {
    return func(backplane *SseBackplane) {
        backplane.channel = channel
    }
}

func WithSseBackplaneLogger(logger loggingcontract.Logger) SseBackplaneOption {
    return func(backplane *SseBackplane) {
        backplane.logger = logger
    }
}

func NewSseBackplane(client rueidis.Client, hub *melodyhttp.SseHub, options ...SseBackplaneOption) *SseBackplane {
    if nil == client {
        exception.Panic(exception.NewError("redis sse backplane client is nil", nil, nil))
    }

    if nil == hub {
        exception.Panic(exception.NewError("redis sse backplane hub is nil", nil, nil))
    }

    ctx, cancel := context.WithCancel(context.Background())

    backplane := &SseBackplane{
        client:  client,
        hub:     hub,
        channel: defaultSseBackplaneChannel,
        origin:  newBackplaneOrigin(),
        ctx:     ctx,
        cancel:  cancel,
    }

    for _, option := range options {
        option(backplane)
    }

    if "" == backplane.channel {
        backplane.channel = defaultSseBackplaneChannel
    }

    hub.SetBackplane(backplane)

    backplane.wait.Add(1)
    go backplane.listen()

    return backplane
}

func (instance *SseBackplane) Publish(topic string, event melodyhttp.SseEvent) error {
    payload, marshalErr := json.Marshal(sseWireEvent{Origin: instance.origin, Topic: topic, Event: event})
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

func (instance *SseBackplane) Close() error {
    instance.cancel()
    instance.wait.Wait()

    return nil
}

func (instance *SseBackplane) listen() {
    defer instance.wait.Done()

    backoff := sseBackplaneInitialBackoff

    for {
        if nil != instance.ctx.Err() {
            return
        }

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

            select {
            case <-time.After(backoff):
            case <-instance.ctx.Done():
                return
            }

            backoff = nextSseBackplaneBackoff(backoff)

            continue
        }

        backoff = sseBackplaneInitialBackoff
    }
}

func (instance *SseBackplane) handle(message rueidis.PubSubMessage) {
    wire := sseWireEvent{}
    if unmarshalErr := json.Unmarshal([]byte(message.Message), &wire); nil != unmarshalErr {
        instance.logError("redis sse backplane could not decode an event", unmarshalErr)

        return
    }

    if wire.Origin == instance.origin {
        return
    }

    instance.hub.DeliverLocal(wire.Topic, wire.Event)
}

func (instance *SseBackplane) logError(message string, err error) {
    if nil == instance.logger {
        return
    }

    instance.logger.Error(message, exception.LogContext(err))
}

func nextSseBackplaneBackoff(current time.Duration) time.Duration {
    next := current * time.Duration(sseBackplaneBackoffFactor)
    if next > sseBackplaneMaxBackoff {
        return sseBackplaneMaxBackoff
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

var _ melodyhttp.SseBackplane = (*SseBackplane)(nil)
