package http

import (
    "sync"
    "sync/atomic"
)

func NewSseHub() *SseHub {
    return &SseHub{
        subscribersByTopic: make(map[string]map[*SseSubscriber]struct{}),
    }
}

type SseHub struct {
    mutex              sync.RWMutex
    subscribersByTopic map[string]map[*SseSubscriber]struct{}
    closed             bool

    dropped uint64
}

type SseSubscriber struct {
    topic   string
    channel chan SseEvent
}

func (instance *SseSubscriber) Events() <-chan SseEvent {
    return instance.channel
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

func (instance *SseHub) Broadcast(topic string, event SseEvent) int {
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
        }
    }

    return delivered
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
