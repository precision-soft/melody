package amqp

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* publishHalf is the publish side of one amqp consumer of this package — the transport and the server-sent-event backplane each embed one — and it is the ONE place the mechanism the two share is written. Both publish the same way: a write on its own goroutine under a publish mutex, because the amqp client discards the context it is handed and holds its send locks across the blocking socket write; a caller that waits for its TURN and then for the WRITE, each under the consumer's budget, because the two stretches fail for different reasons and only the second says anything about the socket; an abandon of a write that outlived its budget, which cuts an owned connection and marks a caller-owned one wedged; and a close that joins the publish half under a bound and reads a failed join together with the writes in flight. The mechanism lived twice, inline, and the two copies drifted inside one window — the turn was separated from the write on one and not the other, a failed join was read alone on one — so this type exists to make the drift impossible to reintroduce: an owner maps the verdicts it is handed onto its own messages, sentinels and dispositions, and writes nothing of the mechanism itself.

   The three fields are the consumer's own, promoted by the embedding: the publish mutex the close joins, the count of writes on the socket, and the wedged flag. The flag is guarded by the OWNER's mutex, not by one of this type's, because it is decided together with the owner's closing flag and connection and read beside them where the owner refuses a publish — one critical section, so the flag and the state it was decided on cannot part. */
type publishHalf struct {
    /* publishMutex serializes the publishes of one consumer. It is taken INSIDE the write goroutine, so a caller that gave up on a wedged write is not itself parked on the mutex that write still holds. */
    publishMutex sync.Mutex

    /* writesInFlight counts the publishes currently inside the amqp client's blocking write. A publish half that a join could not take is BUSY, which is not the same as wedged — it can equally be a healthy confirmation still in its budget, or a broadcast merely queued behind another — and teardown reports and decides on the difference rather than on the join alone. */
    writesInFlight atomic.Int64

    /* wedged is set while a publish write that outlived its budget is still blocked on a connection the owner does not own and so cannot cut: every publish until it returns is refused at once, instead of parking one more goroutine behind it per publish. Guarded by the owner's mutex. */
    wedged bool
}

/* publishAttempt is one publish in flight: the turn protocol between the caller and the write goroutine, the channel the goroutine closes once the socket call returned, and the budget both of the caller's waits are under. */
type publishAttempt struct {
    half    *publishHalf
    budget  time.Duration
    turn    *publishTurn
    written chan struct{}
}

/* beginPublish opens one attempt under the budget both of its waits are measured against. */
func (instance *publishHalf) beginPublish(budget time.Duration) *publishAttempt {
    return &publishAttempt{
        half:    instance,
        budget:  budget,
        turn:    newPublishTurn(),
        written: make(chan struct{}),
    }
}

/* run launches the publish on its own goroutine. Under the publish mutex it takes the turn — and returns without writing when the caller has already given up on the queue — then calls write inside the count of writes in flight, closes written once write returned, and calls after, still under the mutex: the transport's confirmation wait runs there, serialized with the write it confirms, and the backplane hands its outcome over there. */
func (instance *publishAttempt) run(write func(), after func()) {
    go func() {
        instance.half.publishMutex.Lock()
        defer instance.half.publishMutex.Unlock()

        if false == instance.turn.begin() {
            return
        }

        instance.half.writesInFlight.Add(1)
        write()
        instance.half.writesInFlight.Add(-1)
        close(instance.written)

        after()
    }()
}

/* awaitTurn waits for the write goroutine to announce its turn under the budget. It answers true when the budget ran out while the publish was still QUEUED behind the publishes ahead of it — the publish is then abandoned under the turn lock and never written, and the socket was never touched, so nothing may be marked wedged for it — and false once the write has begun, which a timer that fired against a write already under way waits for. */
func (instance *publishAttempt) awaitTurn() (lostInTheQueue bool) {
    turnTimer := time.NewTimer(instance.budget)
    defer turnTimer.Stop()

    select {
    case <-instance.turn.started():
        return false
    case <-turnTimer.C:
        if true == instance.turn.abandon() {
            return true
        }

        <-instance.turn.started()

        return false
    }
}

/* awaitWrite waits for the write to return under the budget and answers whether it did. A budget that ran out is not yet a wedged write: the owner asks writeReturned once more before abandoning, because the two events have no order between them. */
func (instance *publishAttempt) awaitWrite() (returned bool) {
    writeTimer := time.NewTimer(instance.budget)
    defer writeTimer.Stop()

    select {
    case <-instance.written:
        return true
    case <-writeTimer.C:
        return false
    }
}

/* writeReturned is the non-blocking re-read of a write whose budget ran out. The budget expiring and the write ending are two events with no order between them, so the expired branch is reached for a write that finished a moment earlier as readily as for one that is blocked — and the abandon is wrong for a publish that is done: it cuts a healthy connection, reports a fault to a caller whose message the broker has, and names a write nobody is waiting on. The check cannot make the window vanish — a write that returns one instruction later is genuinely still in flight when it is read — and it is not meant to: what it removes is the stretch from the timer firing to the abandon reaching the socket, which is the part a caller can lose a message to. */
func writeReturned(written <-chan struct{}) bool {
    select {
    case <-written:
        return true
    default:
        return false
    }
}

/* publishOwnerState is what the owner knows under its mutex and the abandon is decided on. */
type publishOwnerState struct {
    closing        bool
    ownsConnection bool
    connection     *amqp091.Connection
}

/* wedgedWriteVerdict is what abandonWedgedWrite did about a write that outlived its budget. */
type wedgedWriteVerdict int

const (
    /* wedgedWriteWhileClosing: a write still blocked while the owner closes is the close's to end — it cuts an owned connection itself and cannot cut another's — so nothing is marked and nothing is cut here */
    wedgedWriteWhileClosing wedgedWriteVerdict = iota
    /* wedgedWriteCut: the owned connection was cut with a deadline already passed, which is the one door the amqp client leaves open once its send locks are held — the blocked write returns, the client's shutdown completes, and the owner redials on its next attempt */
    wedgedWriteCut
    /* wedgedWriteOnCallerOwned: nothing here may cut a caller-owned connection, so the owner is marked wedged until the write returns — by the owner's hand, or never — and refuses every publish in between at once */
    wedgedWriteOnCallerOwned
)

/* abandonWedgedWrite is the branch a write that outlived its budget leads to. The owner's state is read and the wedged flag set under the owner's mutex, in one critical section; an owned connection is cut with a deadline already passed and the write is waited for under closeJoinTimeout — it returns as soon as the deadline lands on the socket, and the wait is bounded all the same because a Dial-injected conn that ignores deadlines is not this package's to reason about; on a caller-owned connection a goroutine clears the flag when the write finally returns. */
func (instance *publishHalf) abandonWedgedWrite(stateMutex *sync.Mutex, read func() publishOwnerState, written <-chan struct{}) wedgedWriteVerdict {
    stateMutex.Lock()
    state := read()
    if false == state.closing && false == state.ownsConnection {
        instance.wedged = true
    }
    stateMutex.Unlock()

    if true == state.closing {
        return wedgedWriteWhileClosing
    }

    if true == state.ownsConnection && nil != state.connection {
        _ = state.connection.CloseDeadline(time.Now())

        timer := time.NewTimer(closeJoinTimeout)
        defer timer.Stop()

        select {
        case <-written:
        case <-timer.C:
        }

        return wedgedWriteCut
    }

    go func() {
        <-written

        stateMutex.Lock()
        instance.wedged = false
        stateMutex.Unlock()
    }()

    return wedgedWriteOnCallerOwned
}

/* publishJoin is what the close learned about the publish half: whether it took the publish mutex inside its bound — the caller releases it when it did — and whether a write was on the socket when it looked. */
type publishJoin struct {
    joined        bool
    writeInFlight bool
}

/* wedgedWrite reads the failed join TOGETHER with the writes in flight, never on its own: the publish half is held just as firmly by a healthy confirmation inside its budget, or by a broadcast merely queued behind another, as by a write the peer has stopped reading — and reading the first as the second cut a healthy connection at once and left both channels of a caller-owned one open for good, naming a blocked write that did not exist. */
func (instance publishJoin) wedgedWrite() bool {
    return false == instance.joined && true == instance.writeInFlight
}

/* joinPublishWithin joins the publish half under what is left of the close's budget, bounded by the owner's publish budget: a write still in flight has at most its own budget left before the publish abandons it, and after that the mutex is released when the write returns or never. */
func (instance *publishHalf) joinPublishWithin(closeContext context.Context, budget time.Duration) publishJoin {
    joined := lockWithin(&instance.publishMutex, teardownStretchWithin(closeContext, budget))

    return publishJoin{joined: joined, writeInFlight: 0 < instance.writesInFlight.Load()}
}

/* closeOwnedConnectionWithin closes a connection the owner dialed itself, with a deadline: at once when the join failed over a write that is genuinely in flight — the write is CUT, deliberately, and whatever the client answers about it is the record of that cut — and one budget ahead otherwise, so a clean close handshake gets its round trip while a socket that wedged with nothing in flight, which the join cannot see, still ends inside the same budget. It answers whether the close is to be reported: a close the caller gave NO time is not a close that FAILED. The stretch is zero on every teardown whose budget an earlier component already spent, and the client then cuts the closing handshake at a deadline already behind it and answers an i/o timeout over a live connection the broker was reading — measured 20 times out of 20, where the same connection closed clean with no deadline at all. Reported, it named the connection for a budget somebody else spent, and the teardown's own record already names that budget. A stretch that was POSITIVE and still ran out says something different, and so does the cut: both are reported. */
func (instance *publishHalf) closeOwnedConnectionWithin(closeContext context.Context, budget time.Duration, join publishJoin, connection *amqp091.Connection) (closeErr error, reported bool) {
    cutWedgedWrite := join.wedgedWrite()

    closeStretch := time.Duration(0)
    if false == cutWedgedWrite {
        closeStretch = teardownStretchWithin(closeContext, budget)
    }

    closeErr = ignoringAlreadyClosed(connection.CloseDeadline(time.Now().Add(closeStretch)))

    return closeErr, true == cutWedgedWrite || 0 < closeStretch
}
