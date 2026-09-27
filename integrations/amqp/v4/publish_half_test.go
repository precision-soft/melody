package amqp

import (
    "context"
    "sync"
    "testing"
    "time"
)

/* a publish whose turn never comes inside the budget is reported lost in the QUEUE, and the write goroutine that finally gets the mutex finds its caller gone and writes nothing */
func TestPublishHalf_ATurnThatNeverComesIsLostInTheQueueAndTheWriteIsNeverMade(t *testing.T) {
    half := &publishHalf{}
    half.publishMutex.Lock()
    defer half.publishMutex.Unlock()

    attempt := half.beginPublish(20 * time.Millisecond)

    written := make(chan struct{}, 1)
    attempt.run(func() { written <- struct{}{} }, func() {})

    /* the wait is driven on a goroutine under a timer of its own, so a form that waited for a turn that never comes fails here in two seconds instead of parking the suite */
    lost := make(chan bool, 1)
    go func() { lost <- attempt.awaitTurn() }()

    select {
    case lostInTheQueue := <-lost:
        if false == lostInTheQueue {
            t.Fatal("expected the turn wait to answer lost in the queue while another publish holds the mutex")
        }
    case <-time.After(2 * time.Second):
        t.Fatal("the turn wait did not return within two seconds; a turn that never comes has to be given up on")
    }

    half.publishMutex.Unlock()
    time.Sleep(20 * time.Millisecond)
    half.publishMutex.Lock()

    select {
    case <-written:
        t.Fatal("expected the abandoned publish never written")
    default:
    }
}

/* the write is counted in flight for exactly the write, and after runs once the write returned, still under the mutex */
func TestPublishHalf_TheWriteIsCountedInFlightAndAfterRunsOnceItReturned(t *testing.T) {
    half := &publishHalf{}
    attempt := half.beginPublish(50 * time.Millisecond)

    release := make(chan struct{})
    inWrite := make(chan int64, 1)
    afterRan := make(chan int64, 1)

    attempt.run(func() {
        inWrite <- half.writesInFlight.Load()
        <-release
    }, func() {
        afterRan <- half.writesInFlight.Load()
    })

    if true == attempt.awaitTurn() {
        t.Fatal("expected the turn taken at once on a free mutex")
    }

    if 1 != <-inWrite {
        t.Fatal("expected the write counted in flight while it runs")
    }

    if true == attempt.awaitWrite() {
        t.Fatal("expected the write still in flight past a budget it has not returned within")
    }

    if true == writeReturned(attempt.written) {
        t.Fatal("expected the re-read to answer not returned while the write is held")
    }

    close(release)

    if 0 != <-afterRan {
        t.Fatal("expected the count back to zero before after runs")
    }

    if false == writeReturned(attempt.written) {
        t.Fatal("expected the re-read to answer returned once the write did")
    }
}

/* the budget expiring and the write ending have no order between them: a write that returned is answered as returned by the re-read, whichever case the timer's select chose */
func TestPublishHalf_AwaitWriteAnswersAWriteThatReturnedInsideTheBudget(t *testing.T) {
    half := &publishHalf{}
    attempt := half.beginPublish(time.Second)

    attempt.run(func() {}, func() {})

    if true == attempt.awaitTurn() || false == attempt.awaitWrite() {
        t.Fatal("expected a write that returns at once answered as returned")
    }
}

/* an owned connection the owner has already let go of — nil — is not a caller-owned one: nothing is marked wedged for it, and the write is left to return on its own */
func TestPublishHalf_AbandonOnAnOwnedConnectionAlreadyGoneMarksNothing(t *testing.T) {
    half := &publishHalf{}
    var stateMutex sync.Mutex

    verdict := half.abandonWedgedWrite(&stateMutex, func() publishOwnerState {
        return publishOwnerState{closing: false, ownsConnection: true, connection: nil}
    }, make(chan struct{}))

    if wedgedWriteOnCallerOwned != verdict || true == half.wedged {
        t.Fatalf("expected an owned connection already gone to mark nothing, got verdict %d wedged %v", verdict, half.wedged)
    }
}

func TestPublishHalf_AbandonWhileClosingMarksNothingAndCutsNothing(t *testing.T) {
    half := &publishHalf{}
    var stateMutex sync.Mutex

    verdict := half.abandonWedgedWrite(&stateMutex, func() publishOwnerState {
        return publishOwnerState{closing: true, ownsConnection: false}
    }, make(chan struct{}))

    if wedgedWriteWhileClosing != verdict {
        t.Fatalf("expected the closing verdict, got %d", verdict)
    }

    if true == half.wedged {
        t.Fatal("expected a write still blocked while the owner closes to mark nothing: it is the close's to end")
    }
}

/* on a caller-owned connection nothing can cut the socket: the owner is marked wedged under its own mutex, and the mark is lifted by the write returning — by the owner's hand, or never */
func TestPublishHalf_AbandonOnACallerOwnedConnectionMarksWedgedUntilTheWriteReturns(t *testing.T) {
    half := &publishHalf{}
    var stateMutex sync.Mutex
    written := make(chan struct{})

    verdict := half.abandonWedgedWrite(&stateMutex, func() publishOwnerState {
        return publishOwnerState{closing: false, ownsConnection: false}
    }, written)

    if wedgedWriteOnCallerOwned != verdict {
        t.Fatalf("expected the caller-owned verdict, got %d", verdict)
    }

    stateMutex.Lock()
    wedged := half.wedged
    stateMutex.Unlock()
    if false == wedged {
        t.Fatal("expected the owner marked wedged while the write is blocked on a connection it does not own")
    }

    close(written)

    deadline := time.Now().Add(2 * time.Second)
    for {
        stateMutex.Lock()
        wedged = half.wedged
        stateMutex.Unlock()

        if false == wedged {
            return
        }

        if time.Now().After(deadline) {
            t.Fatal("expected the wedged mark lifted once the write returned")
        }

        time.Sleep(time.Millisecond)
    }
}

/* a failed join is a wedged write only TOGETHER with a write in flight: the publish half is equally held by a confirmation inside its budget or a broadcast queued behind another */
func TestPublishHalf_AFailedJoinIsAWedgedWriteOnlyWithAWriteInFlight(t *testing.T) {
    half := &publishHalf{}
    half.publishMutex.Lock()
    defer half.publishMutex.Unlock()

    join := half.joinPublishWithin(context.Background(), 10*time.Millisecond)
    if true == join.joined || true == join.wedgedWrite() {
        t.Fatalf("expected a failed join over nothing in flight read as busy, not wedged: %+v", join)
    }

    half.writesInFlight.Add(1)
    defer half.writesInFlight.Add(-1)

    join = half.joinPublishWithin(context.Background(), 10*time.Millisecond)
    if true == join.joined || false == join.wedgedWrite() {
        t.Fatalf("expected a failed join over a write in flight read as a wedged write: %+v", join)
    }
}

/* a free publish half is joined whatever the bound says, a spent budget included, and the caller owns the mutex afterwards */
func TestPublishHalf_AFreePublishHalfIsJoinedUnderASpentBudget(t *testing.T) {
    half := &publishHalf{}

    spent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
    defer cancel()

    join := half.joinPublishWithin(spent, time.Second)
    if false == join.joined {
        t.Fatal("expected a mutex nobody holds taken under a spent budget")
    }

    half.publishMutex.Unlock()
}
