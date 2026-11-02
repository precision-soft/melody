package rueidis

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "errors"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"
    "github.com/redis/rueidis"
)

const defaultServerSentEventBackplaneChannel = "melody:sse"

const defaultServerSentEventBackplaneCallTimeout = time.Second

type serverSentEventWireEvent struct {
    Origin string                     `json:"origin"`
    Topic  string                     `json:"topic"`
    Event  melodyhttp.ServerSentEvent `json:"event"`
}

type ServerSentEventBackplane struct {
    client      rueidis.Client
    hub         *melodyhttp.ServerSentEventHub
    channel     string
    origin      string
    logger      loggingcontract.Logger
    reconnect   resolvedReconnect
    callTimeout time.Duration

    ctx    context.Context
    cancel context.CancelFunc
    /* listenerDone is closed by the listener as it leaves, so a close reads its exit without a goroutine of its own */
    listenerDone chan struct{}
}

/* ServerSentEventBackplaneConfig declares the backplane; its zero value is the default backplane. A limit outside its rule is refused before the backplane is installed on the hub or its listener starts. */
type ServerSentEventBackplaneConfig struct {
    /* Channel is the pub/sub channel every instance shares, melody:sse when empty. */
    Channel string

    /* Logger is the journal of subscription failures and reconnects; nil or a typed nil records none. */
    Logger loggingcontract.Logger

    /* Reconnect is the re-subscribe backoff. */
    Reconnect ReconnectConfig

    /* CallTimeout bounds one Publish round trip, so a broadcasting request, whose context typically carries no deadline, fails fast against an unresponsive store: 1s by default, at least 1ms, unlimited refused. */
    CallTimeout bound.Duration
}

var serverSentEventBackplaneCallTimeoutRule = callTimeoutRule("ServerSentEventBackplaneConfig.CallTimeout", defaultServerSentEventBackplaneCallTimeout)

/* NewServerSentEventBackplane resolves the configuration once, then installs the backplane on the hub and starts its listener; a nil client or hub, or a limit outside its rule, panics naming it and leaves the hub without a backplane. */
func NewServerSentEventBackplane(client rueidis.Client, hub *melodyhttp.ServerSentEventHub, config ServerSentEventBackplaneConfig) *ServerSentEventBackplane {
    if nil == client {
        exception.Panic(exception.NewError("redis sse backplane client is nil", nil, nil))
    }

    if nil == hub {
        exception.Panic(exception.NewError("redis sse backplane hub is nil", nil, nil))
    }

    reconnect, reconnectErr := resolveReconnect(config.Reconnect)
    if nil != reconnectErr {
        exception.Panic(exception.FromError(reconnectErr))
    }

    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, serverSentEventBackplaneCallTimeoutRule)
    if nil != callTimeoutErr {
        exception.Panic(exception.FromError(callTimeoutErr))
    }

    channel := config.Channel
    if "" == channel {
        channel = defaultServerSentEventBackplaneChannel
    }

    logger := config.Logger
    if true == isAbsentLogger(logger) {
        logger = nil
    }

    ctx, cancel := context.WithCancel(context.Background())

    backplane := &ServerSentEventBackplane{
        client:       client,
        hub:          hub,
        channel:      channel,
        origin:       newBackplaneOrigin(),
        logger:       logger,
        reconnect:    reconnect,
        callTimeout:  callTimeout.Value,
        ctx:          ctx,
        cancel:       cancel,
        listenerDone: make(chan struct{}),
    }

    hub.SetBackplane(backplane)

    go backplane.listen()

    return backplane
}

func (instance *ServerSentEventBackplane) Publish(topic string, event melodyhttp.ServerSentEvent) error {
    payload, marshalErr := json.Marshal(serverSentEventWireEvent{Origin: instance.origin, Topic: topic, Event: event})
    if nil != marshalErr {
        return exception.NewError("redis sse backplane could not encode the event", map[string]any{"topic": topic}, marshalErr)
    }

    /* the publish is bounded by the call timeout on a context derived from the backplane's own, so a CloseWithContext cancels an in-flight publish too */
    callContext, cancel := context.WithTimeout(instance.ctx, instance.callTimeout)
    defer cancel()

    result := instance.client.Do(
        callContext,
        instance.client.B().Publish().Channel(instance.channel).Message(string(payload)).Build(),
    )
    if resultErr := result.Error(); nil != resultErr {
        return exception.NewError("redis sse backplane publish failed", map[string]any{"topic": topic}, resultErr)
    }

    return nil
}

/* CloseWithContext clears this backplane from the hub, the broadcasts holding it waited for under the deadline, stops the listener and waits for it to leave under the same deadline; past it the listener is left to leave on its own and the deadline's error is answered. */
func (instance *ServerSentEventBackplane) CloseWithContext(closeContext context.Context) error {
    var closeErrs []error

    if clearErr := instance.hub.ClearBackplaneWithContext(closeContext); nil != clearErr {
        closeErrs = append(closeErrs, clearErr)
    }

    instance.cancel()

    select {
    case <-instance.listenerDone:
    case <-closeContext.Done():
        /* a listener that left in the same instant as the deadline is answered as gone, since a select between two ready channels picks at random */
        select {
        case <-instance.listenerDone:
        default:
            closeErrs = append(closeErrs, exception.NewError("redis sse backplane listener did not stop before the close deadline", nil, closeContext.Err()))
        }
    }

    return errors.Join(closeErrs...)
}

func (instance *ServerSentEventBackplane) listen() {
    defer close(instance.listenerDone)

    backoff := instance.reconnect.initialBackoff

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
            backoff = instance.reconnect.initialBackoff

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
    return instance.reconnect.initialBackoff <= subscriptionDuration
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

    instance.logger.Error(message, exception.LogContext(err, exception.LogContextConfig{}))
}

func (instance *ServerSentEventBackplane) nextServerSentEventBackplaneBackoff(current time.Duration) time.Duration {
    /* an extreme factor overflows the product and the duration conversion lands negative; both ends collapse onto the cap so the loop never resubscribes with a zero delay */
    next := time.Duration(float64(current) * instance.reconnect.backoffFactor)
    if next <= 0 || next > instance.reconnect.maxBackoff {
        return instance.reconnect.maxBackoff
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
