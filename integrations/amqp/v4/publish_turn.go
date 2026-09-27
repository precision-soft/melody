package amqp

import "sync"

/* publishTurn separates the two stretches a publish spends before its outcome is known, so a caller that ran out of budget can be told which one it lost. The wait for the turn behind the publishes ahead says nothing about the socket, while the write is the stretch a peer that stopped reading holds; read as one interval, a broadcast that only stood in the queue would be reported as a blocked write, marking the owner wedged and being written later by a goroutine nobody reads. It is a small protocol with two doors because the ordering is the whole of it: the write goroutine announces its turn under the lock the caller gives up under, so exactly one of the two wins. */
type publishTurn struct {
    mutex        sync.Mutex
    writeStarted bool
    callerGaveUp bool
    writing      chan struct{}
}

func newPublishTurn() *publishTurn {
    return &publishTurn{writing: make(chan struct{})}
}

/* begin is called by the write goroutine once it holds the publish mutex and is about to touch the socket. It answers whether the caller is still waiting, since a caller that gave up while queued was told the message did not go out; a publish that proceeds announces its turn, releasing the caller's wait. */
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

/* abandon is called by the caller when the budget for the turn ran out. It answers true when the caller may report a wait lost in the queue, and false when the write had already begun and the caller waits for it instead; begin reads the flag under this lock, so there is no order in which both happen. */
func (instance *publishTurn) abandon() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.writeStarted {
        return false
    }

    instance.callerGaveUp = true

    return true
}

/* started is closed once the write has begun, so a caller whose turn timer fired against a write already under way can wait for that write instead of reporting a queue it has left. */
func (instance *publishTurn) started() <-chan struct{} {
    return instance.writing
}
