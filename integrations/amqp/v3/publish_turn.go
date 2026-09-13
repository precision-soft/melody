package amqp

import "sync"

/* publishTurn separates the two stretches a publish spends before its outcome is known, so a caller that ran out of budget can be told WHICH of them it lost.

   A publish on this connection is serialized behind the publishes ahead of it. The wait for that turn says nothing at all about the socket — the queue in front is the whole of it — while the write is the one stretch a peer that stopped reading actually holds. Read as one interval they are indistinguishable, and a broadcast that only ever stood in the queue is then reported as a blocked write: the transport is marked wedged, every later send is refused at once, and a message the caller was told did not go out is written a moment later by a goroutine nobody is reading any more.

   Both files of this package publish that way, and until now only one of them separated the stretches: the mechanism lived twice, inline, and the copies drifted — which is the defect this type exists to make impossible to reintroduce. It is deliberately a small protocol with two doors rather than a set of fields the caller drives, because the ordering is the whole of it: the write goroutine announces its turn under the same lock the caller gives up under, so exactly one of the two wins and neither can observe a half-written decision. */
type publishTurn struct {
    mutex        sync.Mutex
    writeStarted bool
    callerGaveUp bool
    writing      chan struct{}
}

func newPublishTurn() *publishTurn {
    return &publishTurn{writing: make(chan struct{})}
}

/* begin is called by the write goroutine at the moment it holds the publish mutex and is about to touch the socket. It answers whether the caller is still reading: a caller that gave up while this publish was queued has already been told the message did not go out, so writing it now would put on the wire an event that was counted as a failure. A publish that may proceed announces its turn, which is what releases the caller from its own wait. */
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

/* abandon is called by the caller when the budget for the turn ran out. It answers whether the caller may report a wait it lost in the QUEUE — true — or whether the write had already begun, in which case the caller has nothing to give up and waits for the write instead. The flag it sets is read by begin under this same lock, so a write that starts in the same instant either sees the caller gone or is seen as started; there is no order in which both happen. */
func (instance *publishTurn) abandon() bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if true == instance.writeStarted {
        return false
    }

    instance.callerGaveUp = true

    return true
}

/* started is closed once the write has begun, so a caller whose turn timer fired against a write already under way can wait for that write instead of reporting a queue it is no longer in. */
func (instance *publishTurn) started() <-chan struct{} {
    return instance.writing
}
