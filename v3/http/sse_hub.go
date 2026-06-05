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

    /** dropped counts events discarded because a subscriber's buffer was full; delivery is
    at-most-once, so a slow consumer loses events rather than blocking the broadcaster. */
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

/** DroppedEventCount returns the cumulative number of events discarded across all topics because a
subscriber's buffer was full. It lets callers surface the at-most-once delivery loss as a metric. */
func (instance *SseHub) DroppedEventCount() uint64 {
    return atomic.LoadUint64(&instance.dropped)
}

func (instance *SseHub) SubscriberCount(topic string) int {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return len(instance.subscribersByTopic[topic])
}
