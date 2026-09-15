package amqp

import "sync"

type publishTurn struct {
    mutex        sync.Mutex
    writeStarted bool
    callerGaveUp bool
    writing      chan struct{}
}

func newPublishTurn() *publishTurn {
    return &publishTurn{writing: make(chan struct{})}
}

func (instance *publishTurn) begin() bool {
    instance.mutex.Lock()

    if true == instance.callerGaveUp {
        instance.mutex.Unlock()

        return false
    }

    instance.writeStarted = true
    instance.mutex.Unlock()

    close(instance.writing)

    return true
}

func (instance *publishTurn) abandon() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.writeStarted {
        return false
    }

    instance.callerGaveUp = true

    return true
}

func (instance *publishTurn) started() <-chan struct{} {
    return instance.writing
}
