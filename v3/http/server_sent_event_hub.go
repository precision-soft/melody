package http

import (
    "sync"
    "sync/atomic"
)

type ServerSentEventBackplane interface {
    Publish(topic string, event ServerSentEvent) error

    Close() error
}

func NewServerSentEventHub() *ServerSentEventHub {
    return &ServerSentEventHub{
        subscribersByTopic: make(map[string]map[*ServerSentEventSubscriber]struct{}),
    }
}

type ServerSentEventHub struct {
    mutex              sync.RWMutex
    subscribersByTopic map[string]map[*ServerSentEventSubscriber]struct{}
    closed             bool
    backplane          ServerSentEventBackplane

    dropped           uint64
    backplaneFailures uint64
}

type ServerSentEventSubscriber struct {
    topic   string
    channel chan ServerSentEvent
    dropped uint64
}

func (instance *ServerSentEventSubscriber) Events() <-chan ServerSentEvent {
    return instance.channel
}

func (instance *ServerSentEventSubscriber) DroppedCount() uint64 {
    return atomic.LoadUint64(&instance.dropped)
}

func (instance *ServerSentEventHub) Subscribe(topic string, bufferSize int) *ServerSentEventSubscriber {
    if 0 >= bufferSize {
        bufferSize = 16
    }

    subscriber := &ServerSentEventSubscriber{
        topic:   topic,
        channel: make(chan ServerSentEvent, bufferSize),
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        close(subscriber.channel)

        return subscriber
    }

    subscribers, exists := instance.subscribersByTopic[topic]
    if false == exists {
        subscribers = make(map[*ServerSentEventSubscriber]struct{})
        instance.subscribersByTopic[topic] = subscribers
    }

    subscribers[subscriber] = struct{}{}

    return subscriber
}

func (instance *ServerSentEventHub) Unsubscribe(subscriber *ServerSentEventSubscriber) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    subscribers, exists := instance.subscribersByTopic[subscriber.topic]
    if false == exists {
        return
    }

    if _, found := subscribers[subscriber]; false == found {
        return
    }

    delete(subscribers, subscriber)
    close(subscriber.channel)

    if 0 == len(subscribers) {
        delete(instance.subscribersByTopic, subscriber.topic)
    }
}

func (instance *ServerSentEventHub) SetBackplane(backplane ServerSentEventBackplane) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.backplane = backplane
}

func (instance *ServerSentEventHub) Broadcast(topic string, event ServerSentEvent) int {
    delivered := instance.DeliverLocal(topic, event)

    instance.replicate(topic, event)

    return delivered
}

func (instance *ServerSentEventHub) DeliverLocal(topic string, event ServerSentEvent) int {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    subscribers, exists := instance.subscribersByTopic[topic]
    if false == exists {
        return 0
    }

    delivered := 0
    for subscriber := range subscribers {
        select {
        case subscriber.channel <- event:
            delivered++
        default:
            atomic.AddUint64(&instance.dropped, 1)
            atomic.AddUint64(&subscriber.dropped, 1)
        }
    }

    return delivered
}

func (instance *ServerSentEventHub) BackplaneFailures() uint64 {
    return atomic.LoadUint64(&instance.backplaneFailures)
}

func (instance *ServerSentEventHub) DroppedEventCount() uint64 {
    return atomic.LoadUint64(&instance.dropped)
}

func (instance *ServerSentEventHub) SubscriberCount(topic string) int {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return len(instance.subscribersByTopic[topic])
}

func (instance *ServerSentEventHub) Shutdown() {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        return
    }

    instance.closed = true

    for topic, subscribers := range instance.subscribersByTopic {
        for subscriber := range subscribers {
            close(subscriber.channel)
        }

        delete(instance.subscribersByTopic, topic)
    }
}

func (instance *ServerSentEventHub) replicate(topic string, event ServerSentEvent) {
    instance.mutex.RLock()
    closed := instance.closed
    backplane := instance.backplane
    instance.mutex.RUnlock()

    if true == closed {
        return
    }

    if nil == backplane {
        return
    }

    if publishErr := backplane.Publish(topic, event); nil != publishErr {
        atomic.AddUint64(&instance.backplaneFailures, 1)
    }
}
