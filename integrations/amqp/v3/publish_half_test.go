package amqp

import (
    "context"
    "strings"
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
    go func() { lost <- attempt.awaitTurn(context.Background()) }()

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

    if true == attempt.awaitTurn(context.Background()) {
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

    if true == attempt.awaitTurn(context.Background()) || false == attempt.awaitWrite() {
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

/* the cut of a wedged write on an owned connection returns within its bound even when the client's own shutdown already began: that shutdown holds the connection mutex the client's close takes first, and waits for the write, so a cut made in line would hold the publish until the write returns, which here is never inside the test. The write the cut did not free is told apart, and the owner marked wedged until it returns */
func TestPublishHalf_AbandonOnAnOwnedConnectionWhoseCutDidNotFreeTheWriteReturnsWithinTheBoundAndMarksItWedged(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    half := &publishHalf{}
    var stateMutex sync.Mutex
    bound := 300 * time.Millisecond

    verdicts := make(chan wedgedWriteVerdict, 1)
    started := time.Now()
    go func() {
        verdicts <- half.abandonWedgedWriteWithin(&stateMutex, func() publishOwnerState {
            return publishOwnerState{closing: false, ownsConnection: true, connection: wedge.connection}
        }, wedge.written, bound)
    }()

    select {
    case verdict := <-verdicts:
        if wedgedWriteCutUnfreed != verdict {
            t.Fatalf("expected the verdict of a cut that did not free the write, got %d", verdict)
        }
    case <-time.After(bound + 2*time.Second):
        t.Fatalf("the abandon did not return within its bound %s plus two seconds; the cut is held behind the client's stalled shutdown", bound)
    }

    stateMutex.Lock()
    wedged := half.wedged
    stateMutex.Unlock()

    if false == wedged {
        t.Fatal("expected the owner marked wedged while the write the cut did not free is still blocked")
    }

    elapsed := time.Since(started)
    t.Logf("abandon returned after %s under a bound of %s", elapsed, bound)

    if true == writeReturned(wedge.written) {
        t.Fatal("the wedged write returned, so the client's shutdown was not stalled behind it and nothing was measured")
    }
}

/* the control: with no shutdown of the client's in progress, the cut reaches the socket and the wedged write returns inside the bound, as before */
func TestPublishHalf_AbandonOnAnOwnedConnectionUnblocksTheWriteWhenNoShutdownIsInProgress(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)

    half := &publishHalf{}
    var stateMutex sync.Mutex

    started := time.Now()
    verdict := half.abandonWedgedWriteWithin(&stateMutex, func() publishOwnerState {
        return publishOwnerState{closing: false, ownsConnection: true, connection: wedge.connection}
    }, wedge.written, 2*time.Second)

    if wedgedWriteCut != verdict {
        t.Fatalf("expected the cut verdict, got %d", verdict)
    }

    if false == writeReturned(wedge.written) {
        t.Fatalf("the cut did not unblock the wedged write within the bound (%s elapsed)", time.Since(started))
    }

    stateMutex.Lock()
    defer stateMutex.Unlock()

    if true == half.wedged {
        t.Fatal("a cut that freed the write marks nothing wedged")
    }

    t.Logf("the cut unblocked the write after %s", time.Since(started))
}

/* the close of an owned connection returns within the caller's deadline, and reports that the client's close did not, even when the client's own shutdown already began and holds the connection mutex behind the wedged write */
func TestPublishHalf_CloseOwnedConnectionReturnsWithinTheBoundWhileTheClientShutdownIsStalled(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)
    wedge.beginClientShutdown(t)

    half := &publishHalf{}
    bound := 300 * time.Millisecond

    closeContext, cancel := context.WithTimeout(context.Background(), bound)
    defer cancel()

    type closeOutcome struct {
        closeErr error
        reported bool
        returned bool
    }

    outcomes := make(chan closeOutcome, 1)
    started := time.Now()
    go func() {
        closeErr, reported, returned := half.closeOwnedConnectionWithin(closeContext, time.Second, publishJoin{joined: false, writeInFlight: true}, wedge.connection)
        outcomes <- closeOutcome{closeErr: closeErr, reported: reported, returned: returned}
    }()

    select {
    case outcome := <-outcomes:
        t.Logf("close returned after %s under a deadline of %s: %v", time.Since(started), bound, outcome.closeErr)

        if false == outcome.reported || true == outcome.returned || nil == outcome.closeErr || false == strings.Contains(outcome.closeErr.Error(), "did not return within the bound") {
            t.Fatalf("expected the close that did not return reported as such, got reported %v returned %v error %v", outcome.reported, outcome.returned, outcome.closeErr)
        }
    case <-time.After(bound + 2*time.Second):
        t.Fatalf("the close did not return within the deadline %s plus two seconds; it is held behind the client's stalled shutdown", bound)
    }
}

/* the control: with no shutdown of the client's in progress, the cut of the close reaches the socket, the wedged write returns, and what the client answered is what is reported */
func TestPublishHalf_CloseOwnedConnectionCutsTheWriteWhenNoShutdownIsInProgress(t *testing.T) {
    wedge := wedgeAPublishOnAFakeBroker(t)

    half := &publishHalf{}

    closeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()

    closeErr, reported, returned := half.closeOwnedConnectionWithin(closeContext, time.Second, publishJoin{joined: false, writeInFlight: true}, wedge.connection)

    if false == reported || false == returned || nil == closeErr || true == strings.Contains(closeErr.Error(), "did not return within the bound") {
        t.Fatalf("expected the client's own answer to the cut reported, got reported %v returned %v error %v", reported, returned, closeErr)
    }

    select {
    case <-wedge.written:
    case <-time.After(2 * time.Second):
        t.Fatal("the cut did not unblock the wedged write")
    }
}

func TestPublishHalf_AwaitTurnEndsWhenTheCallersContextIsCancelled(t *testing.T) {
    half := &publishHalf{}
    release := holdPublishMutex(t, &half.publishMutex)
    defer release()

    attempt := half.beginPublish(5 * time.Second)

    wrote := make(chan struct{}, 1)
    attempt.run(func() { wrote <- struct{}{} }, func() {})

    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    started := time.Now()
    if false == attempt.awaitTurn(ctx) {
        t.Fatal("expected the publish lost in the queue once the caller's context was cancelled")
    }

    if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
        t.Fatalf("expected the turn to end at the cancellation, it took %s", elapsed)
    }

    release()

    select {
    case <-wrote:
        t.Fatal("the publish abandoned in the queue was written")
    case <-time.After(100 * time.Millisecond):
    }
}

func TestCloseConnectionWithin_ReportsTheClientsOwnTimeoutAgainstASilentBroker(t *testing.T) {
    for run := 0; run < 20; run = run + 1 {
        fake := dialFakeBroker(t)

        closeErr, returned := closeConnectionWithin(context.Background(), 100*time.Millisecond, fake.connection)

        if false == returned || nil == closeErr || true == strings.Contains(closeErr.Error(), "did not return within the bound") {
            t.Fatalf("run %d: expected the client's own timeout received, got returned %v error %v", run, returned, closeErr)
        }
    }
}

func TestCloseConnectionWithin_UnderADeadlineWithNoRoomArmsTheDeadlineEarlier(t *testing.T) {
    fake := dialFakeBroker(t)

    closeContext, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
    defer cancel()

    closeErr, returned := closeConnectionWithin(closeContext, teardownStretchWithin(closeContext, time.Second), fake.connection)

    if false == returned || nil == closeErr || true == strings.Contains(closeErr.Error(), "did not return within the bound") {
        t.Fatalf("expected the client's own timeout inside the caller's deadline, got returned %v error %v", returned, closeErr)
    }
}
