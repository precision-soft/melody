package http

import (
    "sync"
    "sync/atomic"
)

type SseBackplane interface {
    Publish(topic string, event SseEvent) error

    Close() error
}

func NewSseHub() *SseHub {
    return &SseHub{
        subscribersByTopic: make(map[string]map[*SseSubscriber]struct{}),
    }
}

type SseHub struct {
    mutex              sync.RWMutex
    subscribersByTopic map[string]map[*SseSubscriber]struct{}
    closed             bool
    backplane          SseBackplane

    dropped           uint64
    backplaneFailures uint64
}

type SseSubscriber struct {
    topic   string
    channel chan SseEvent
    dropped uint64
}

func (instance *SseSubscriber) Events() <-chan SseEvent {
    return instance.channel
}

func (instance *SseSubscriber) DroppedCount() uint64 {
    return atomic.LoadUint64(&instance.dropped)
}

func (instance *SseHub) Subscribe(topic string, bufferSize int) *SseSubscriber {
    if 0 >= bufferSize {
        bufferSize = 16
    }

    subscriber := &SseSubscriber{
        topic:   topic,
        channel: make(chan SseEvent, bufferSize),
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.closed {
        close(subscriber.channel)

        return subscriber
    }

    subscribers, exists := instance.subscribersByTopic[topic]
    if false == exists {
        subscribers = make(map[*SseSubscriber]struct{})
        instance.subscribersByTopic[topic] = subscribers
    }

    subscribers[subscriber] = struct{}{}

    return subscriber
}

func (instance *SseHub) Unsubscribe(subscriber *SseSubscriber) {
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

func (instance *SseHub) SetBackplane(backplane SseBackplane) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.backplane = backplane
}

func (instance *SseHub) Broadcast(topic string, event SseEvent) int {
    delivered := instance.DeliverLocal(topic, event)

    instance.replicate(topic, event)

    return delivered
}

func (instance *SseHub) DeliverLocal(topic string, event SseEvent) int {
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

func (instance *SseHub) replicate(topic string, event SseEvent) {
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

func (instance *SseHub) BackplaneFailures() uint64 {
    return atomic.LoadUint64(&instance.backplaneFailures)
}

func (instance *SseHub) DroppedEventCount() uint64 {
    return atomic.LoadUint64(&instance.dropped)
}

func (instance *SseHub) SubscriberCount(topic string) int {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return len(instance.subscribersByTopic[topic])
}

func (instance *SseHub) Shutdown() {
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
