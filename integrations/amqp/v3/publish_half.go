package amqp

import (
    "context"
    "sync"
    "sync/atomic"
    "time"

    "github.com/precision-soft/melody/v3/exception"
    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* publishHalf is the publish side of one amqp consumer of this package, embedded by the transport and the server-sent-event backplane so the mechanism is written once: a write on its own goroutine under a publish mutex, because the amqp client discards the context and holds its send locks across the blocking write; a caller that waits for its turn and then for the write, each under the consumer's budget, since only the second says anything about the socket; an abandon of a write that outlived its budget, cutting an owned connection or marking a caller-owned one wedged; and a close that joins the publish half under a bound and reads a failed join together with the writes in flight. An owner maps the verdicts onto its own messages, sentinels and dispositions. The wedged flag is guarded by the owner's mutex, because it is decided together with the owner's closing flag and connection in one critical section. */
type publishHalf struct {
    /* publishMutex serialises the publishes of one consumer. It is taken inside the write goroutine, so a caller that gave up on a wedged write is not parked on the mutex that write still holds. */
    publishMutex sync.Mutex

    /* writesInFlight counts the publishes inside the amqp client's blocking write. A publish half a join could not take is busy, which may be a healthy confirmation or a queued broadcast as well as a wedged write, so teardown decides on this count rather than on the join alone. */
    writesInFlight atomic.Int64

    /* wedged is set while a publish write that outlived its budget is still blocked on a connection the owner cannot end: one it does not own and so cannot cut, or one it cut whose write the cut did not free inside the bound. Every publish until the write returns is refused at once, instead of parking one more goroutine behind it per publish. Guarded by the owner's mutex. */
    wedged bool

    /* joinBound is the bound on the waits for a call into the amqp client that may be parked behind a wedged write: the cut write, a channel close. Zero is closeJoinTimeout. */
    joinBound time.Duration
}

/* closeGrace is how long a wait on the client's CloseDeadline outlives the deadline it armed, so the client's own timeout is the answer received rather than an expired timer racing it */
const closeGrace = 250 * time.Millisecond

/* resolvedJoinBound is joinBound read with its default. */
func (instance *publishHalf) resolvedJoinBound() time.Duration {
    return positiveOrDefault(instance.joinBound, closeJoinTimeout)
}

/* publishAttempt is one publish in flight: the turn protocol between the caller and the write goroutine, the channel the goroutine closes once the socket call returned, and the budget both of the caller's waits are under. */
type publishAttempt struct {
    half    *publishHalf
    budget  time.Duration
    turn    *publishTurn
    written chan struct{}
}

/* beginPublish opens one attempt under the budget both of its waits run under. */
func (instance *publishHalf) beginPublish(budget time.Duration) *publishAttempt {
    return &publishAttempt{
        half:    instance,
        budget:  budget,
        turn:    newPublishTurn(),
        written: make(chan struct{}),
    }
}

/* run launches the publish on its own goroutine. Under the publish mutex it takes the turn, returning without writing when the caller already gave up on the queue, then calls write inside the count of writes in flight, closes written once write returned, and calls after still under the mutex, where the transport's confirmation wait is serialised with the write it confirms. */
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

/* awaitTurn waits for the write goroutine to announce its turn under the budget and the caller's context. It answers true when the budget ran out or the context ended while the publish was still queued, which abandons it unwritten with the socket untouched, so nothing may be marked wedged; and false once the write has begun, which the caller then waits for. */
func (instance *publishAttempt) awaitTurn(ctx context.Context) (lostInTheQueue bool) {
    turnTimer := time.NewTimer(instance.budget)
    defer turnTimer.Stop()

    abandon := func() bool {
        if true == instance.turn.abandon() {
            return true
        }

        <-instance.turn.started()

        return false
    }

    select {
    case <-instance.turn.started():
        return false
    case <-turnTimer.C:
        return abandon()
    case <-ctx.Done():
        return abandon()
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

/* writeReturned is the non-blocking re-read of a write whose budget ran out. The budget expiring and the write ending have no order between them, and abandoning a write that is done would cut a healthy connection and report a fault for a message the broker has; the check closes the stretch from the timer firing to the abandon reaching the socket. */
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
    /* wedgedWriteCutUnfreed: the owned connection was cut but the write did not return inside the bound, because a shutdown the client already began holds the connection mutex behind it; the channel is left to end with the connection, since its close would park on the mutex the write holds, and the owner is marked wedged until the write returns */
    wedgedWriteCutUnfreed
)

/* abandonWedgedWrite is the branch a write that outlived its budget leads to, with the wait for the cut write bounded by the join bound. */
func (instance *publishHalf) abandonWedgedWrite(stateMutex *sync.Mutex, read func() publishOwnerState, written <-chan struct{}) wedgedWriteVerdict {
    return instance.abandonWedgedWriteWithin(stateMutex, read, written, instance.resolvedJoinBound())
}

/* abandonWedgedWriteWithin is abandonWedgedWrite under the bound its caller names for the wait on a cut write. The owner's state is read and the wedged flag set under the owner's mutex in one critical section. An owned connection is cut with a deadline already passed and the write waited for under the bound, which is armed at once: the cut runs on a goroutine of its own, because the client's CloseDeadline first takes the connection mutex, and a shutdown the client already began — its reader failed, a missed heartbeat or a peer gone — holds that mutex while it waits for the channel mutex the wedged write holds across the socket call, so a cut made in line would block until the write returns, which on a peer that stopped reading is never inside any budget. In that state nothing in the client can reach the socket, so the write ends only when the operating system fails it, and the goroutine stays parked until then and the client's shutdown completes; that is the connection ending, not a leak of one goroutine per publish, since later publishes queue behind the wedged write and are abandoned in the queue. The wait is bounded as well because a Dial-injected conn may ignore deadlines. On a caller-owned connection a goroutine clears the flag when the write returns. */
func (instance *publishHalf) abandonWedgedWriteWithin(stateMutex *sync.Mutex, read func() publishOwnerState, written <-chan struct{}, joinBound time.Duration) wedgedWriteVerdict {
    stateMutex.Lock()
    state := read()
    if false == state.closing && false == state.ownsConnection {
        instance.wedged = true
    }
    stateMutex.Unlock()

    if true == state.closing {
        return wedgedWriteWhileClosing
    }

    /* no owner produces an owned connection that is nil, since the transport nils it only inside its close after closing is raised and the backplane never does, so this branch answers the caller-owned verdict with nothing marked */
    if true == state.ownsConnection && nil != state.connection {
        connection := state.connection
        cutAt := time.Now()

        go func() {
            _ = connection.CloseDeadline(cutAt)
        }()

        timer := time.NewTimer(joinBound)
        defer timer.Stop()

        select {
        case <-written:
            return wedgedWriteCut
        case <-timer.C:
        }

        /* the write and the timer can become ready in the same instant and select picks at random, so a write that returned is preferred over an expired bound */
        if true == writeReturned(written) {
            return wedgedWriteCut
        }

        stateMutex.Lock()
        instance.wedged = true
        stateMutex.Unlock()

        instance.clearWedgedWhenWritten(stateMutex, written)

        return wedgedWriteCutUnfreed
    }

    instance.clearWedgedWhenWritten(stateMutex, written)

    return wedgedWriteOnCallerOwned
}

/* clearWedgedWhenWritten clears the wedged flag, on a goroutine of its own, once the blocked write returned. */
func (instance *publishHalf) clearWedgedWhenWritten(stateMutex *sync.Mutex, written <-chan struct{}) {
    go func() {
        <-written

        stateMutex.Lock()
        instance.wedged = false
        stateMutex.Unlock()
    }()
}

/* publishJoin is what the close learned about the publish half: whether it took the publish mutex inside its bound — the caller releases it when it did — and whether a write was on the socket when it looked. */
type publishJoin struct {
    joined        bool
    writeInFlight bool
}

/* wedgedWrite reads the failed join together with the writes in flight, never alone: the publish half is held as firmly by a healthy confirmation or a queued broadcast as by a write the peer stopped reading, and only the last is a wedged write. */
func (instance publishJoin) wedgedWrite() bool {
    return false == instance.joined && true == instance.writeInFlight
}

/* joinPublishWithin joins the publish half under what is left of the close's budget, bounded by the owner's publish budget: a write still in flight has at most its own budget left before the publish abandons it, and after that the mutex is released when the write returns or never. */
func (instance *publishHalf) joinPublishWithin(closeContext context.Context, budget time.Duration) publishJoin {
    joined := lockWithin(&instance.publishMutex, teardownStretchWithin(closeContext, budget))

    return publishJoin{joined: joined, writeInFlight: 0 < instance.writesInFlight.Load()}
}

/* closeOwnedConnectionWithin closes a connection the owner dialed, with a deadline: at once when the join failed over a write genuinely in flight, which is cut deliberately, and one budget ahead otherwise, so a clean close handshake gets its round trip while a socket wedged with nothing in flight still ends inside the budget. The close runs through closeConnectionWithin, so it returns within its bound even behind a shutdown the client already began; a close that did not return answers returned false, on which the owner leaves the channels to end with the connection, since a channel close takes the same channel and connection mutexes and would block the same way. It answers whether the close is to be reported: with a zero stretch, a budget an earlier component spent, the client cuts the handshake and answers an i/o timeout over a live connection, which is not reported, and neither is a close the caller left no time to return; a positive stretch that ran out, and the cut, are. */
func (instance *publishHalf) closeOwnedConnectionWithin(closeContext context.Context, budget time.Duration, join publishJoin, connection *amqp091.Connection) (closeErr error, reported bool, returned bool) {
    cutWedgedWrite := join.wedgedWrite()

    closeStretch := time.Duration(0)
    if false == cutWedgedWrite {
        closeStretch = teardownStretchWithin(closeContext, budget)
    }

    reported = true == cutWedgedWrite || 0 < closeStretch

    closeErr, returned = closeConnectionWithin(closeContext, closeStretch, connection)
    if true == returned {
        closeErr = ignoringAlreadyClosed(closeErr)
    }

    return closeErr, reported, returned
}

/* closeConnectionWithin runs the client's CloseDeadline on a goroutine of its own and waits for it under a bound, answering the client's answer and returned true, or an error naming the bound and returned false. A positive stretch arms the deadline that far ahead and the wait outlives it by closeGrace, so the client's own timeout is what is reported; under a caller's deadline that leaves no room past the stretch, the deadline is armed earlier instead, keeping the grace inside it, and a deadline that leaves less than two graces is split in half, so the close frame is still written and the handshake still tried. A zero stretch is a cut, whose deadline is already behind it, waited for under the join timeout within what is left of the caller's deadline. The client's close first takes the connection mutex, and a shutdown the client already began holds it while it waits for the channel mutex a wedged write holds across the socket call, so a close made in line would block until that write returns; the goroutine ends when the client's shutdown completes. */
func closeConnectionWithin(closeContext context.Context, stretch time.Duration, connection *amqp091.Connection) (closeErr error, returned bool) {
    armedStretch := stretch
    closeWait := teardownStretchWithin(closeContext, closeJoinTimeout)

    if 0 < stretch {
        closeWait = teardownStretchWithin(closeContext, stretch+closeGrace)
        if closeWait-armedStretch < closeGrace {
            armedStretch = max(closeWait-closeGrace, closeWait/2)
        }
    }

    deadline := time.Now().Add(armedStretch)
    outcome := make(chan error, 1)

    go func() {
        outcome <- connection.CloseDeadline(deadline)
    }()

    timer := time.NewTimer(closeWait)
    defer timer.Stop()

    select {
    case closeErr = <-outcome:
        return closeErr, true
    case <-timer.C:
        /* the close and the timer can become ready in the same instant and select picks at random, so an answer that exists is preferred over an expired bound */
        select {
        case closeErr = <-outcome:
            return closeErr, true
        default:
        }

        return exception.NewError(
            "amqp connection close did not return within the bound; the client's close ends when its shutdown completes",
            map[string]any{"bound": closeWait.String()},
            nil,
        ), false
    }
}
