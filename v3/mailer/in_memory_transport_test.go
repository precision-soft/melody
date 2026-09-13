package mailer

import (
    "sync"
    "testing"

    mailercontract "github.com/precision-soft/melody/v3/mailer/contract"
)

func TestInMemoryTransport_KeepsWhatItWasSentInOrder(t *testing.T) {
    transport := NewInMemoryTransport()

    if 0 != len(transport.Sent()) {
        t.Fatalf("expected a fresh transport to have sent nothing, got %v", transport.Sent())
    }

    if sendErr := transport.Send(nil, mailercontract.Message{Subject: "first"}); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    if sendErr := transport.Send(nil, mailercontract.Message{Subject: "second"}); nil != sendErr {
        t.Fatalf("unexpected send error: %v", sendErr)
    }

    sent := transport.Sent()
    if 2 != len(sent) || "first" != sent[0].Subject || "second" != sent[1].Subject {
        t.Fatalf("expected both messages in the order they were sent, got %v", sent)
    }
}

/* the recorded list is handed out as a copy: a test holding the slice while the application keeps sending would otherwise be reading a backing array the transport appends into under it */
func TestInMemoryTransport_SentHandsOutACopy(t *testing.T) {
    transport := NewInMemoryTransport()
    _ = transport.Send(nil, mailercontract.Message{Subject: "first"})

    read := transport.Sent()
    read[0].Subject = "rewritten"

    if "first" != transport.Sent()[0].Subject {
        t.Fatalf("expected the transport to keep its own copy, got %q", transport.Sent()[0].Subject)
    }

    _ = transport.Send(nil, mailercontract.Message{Subject: "second"})

    if 1 != len(read) {
        t.Fatalf("expected the earlier read to stay the length it was, got %v", read)
    }
}

/* the transport stands in for a real one in tests that run handlers concurrently, so its own bookkeeping must be safe under the race detector — the guard is the mutex, and without it this is exactly the append that corrupts */
func TestInMemoryTransport_IsSafeUnderConcurrentSendAndRead(t *testing.T) {
    transport := NewInMemoryTransport()

    var waitGroup sync.WaitGroup
    for index := 0; index < 8; index++ {
        waitGroup.Add(2)

        go func() {
            defer waitGroup.Done()

            _ = transport.Send(nil, mailercontract.Message{Subject: "concurrent"})
        }()

        go func() {
            defer waitGroup.Done()

            _ = transport.Sent()
        }()
    }

    waitGroup.Wait()

    if 8 != len(transport.Sent()) {
        t.Fatalf("expected every concurrent send to be recorded, got %d", len(transport.Sent()))
    }
}
