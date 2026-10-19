package messagebus

import (
    "testing"
    "time"

    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

/* the stamp names are the wire form every transport and every middleware reads a stamp back by: a name that drifts here is a stamp nobody finds again, and nothing in the type system says so */
func TestStampNames_AreTheWireNames(t *testing.T) {
    for _, probe := range []struct {
        stamp    messagebuscontract.Stamp
        expected string
    }{
        {stamp: BusNameStamp{}, expected: "bus_name"},
        {stamp: SentStamp{}, expected: "sent"},
        {stamp: ReceivedStamp{}, expected: "received"},
        {stamp: HandledStamp{}, expected: "handled"},
        {stamp: RedeliveryStamp{}, expected: "redelivery"},
        {stamp: DelayStamp{}, expected: "delay"},
        {stamp: DeadLetterAttemptStamp{}, expected: "dead_letter_attempt"},
        {stamp: MessageIdStamp{}, expected: "message_id"},
    } {
        if probe.expected != probe.stamp.StampName() {
            t.Fatalf("%T: expected %q, got %q", probe.stamp, probe.expected, probe.stamp.StampName())
        }
    }
}

/* an envelope accumulates stamps of the same type as it travels — a redelivery stamps again on every attempt — so the reader must answer the LAST one; answering the first would freeze every count at its opening value */
func TestLastStampOfType_AnswersTheMostRecentStampOfItsType(t *testing.T) {
    envelopeInstance := NewEnvelope(
        "payload",
        RedeliveryStamp{Count: 1},
        SentStamp{TransportName: "amqp"},
        RedeliveryStamp{Count: 3},
    )

    stamp, found := LastStampOfType[RedeliveryStamp](envelopeInstance)
    if false == found {
        t.Fatal("expected the redelivery stamp to be found")
    }

    if 3 != stamp.Count {
        t.Fatalf("expected the last redelivery stamp, got %d", stamp.Count)
    }
}

func TestLastStampOfType_AnswersFalseWhenTheTypeIsAbsent(t *testing.T) {
    stamp, found := LastStampOfType[DelayStamp](NewEnvelope("payload", SentStamp{TransportName: "amqp"}))
    if true == found {
        t.Fatalf("expected no delay stamp, got %v", stamp)
    }

    if (DelayStamp{}) != stamp {
        t.Fatalf("expected the zero stamp when absent, got %v", stamp)
    }
}

/* the counters answer zero for an unstamped envelope rather than refusing: a message on its first delivery carries no redelivery stamp at all, and every caller treats the answer as an attempt number */
func TestStampCounters_ReadZeroOnAnUnstampedEnvelope(t *testing.T) {
    bare := NewEnvelope("payload")

    if 0 != RedeliveryCount(bare) {
        t.Fatalf("expected no redeliveries on a bare envelope, got %d", RedeliveryCount(bare))
    }

    if 0 != DeadLetterAttemptCount(bare) {
        t.Fatalf("expected no dead letter attempts on a bare envelope, got %d", DeadLetterAttemptCount(bare))
    }

    if messageId, found := MessageId(bare); true == found || "" != messageId {
        t.Fatalf("expected no message id on a bare envelope, got %q", messageId)
    }
}

func TestStampCounters_ReadTheLastStampedValue(t *testing.T) {
    stamped := NewEnvelope("payload").
        WithStamp(RedeliveryStamp{Count: 1}, DeadLetterAttemptStamp{Count: 1}).
        WithStamp(RedeliveryStamp{Count: 2}, DeadLetterAttemptStamp{Count: 4}, MessageIdStamp{MessageId: "order-1"}, DelayStamp{Delay: time.Second})

    if 2 != RedeliveryCount(stamped) {
        t.Fatalf("expected the last redelivery count, got %d", RedeliveryCount(stamped))
    }

    if 4 != DeadLetterAttemptCount(stamped) {
        t.Fatalf("expected the last dead letter attempt count, got %d", DeadLetterAttemptCount(stamped))
    }

    messageId, found := MessageId(stamped)
    if false == found || "order-1" != messageId {
        t.Fatalf("expected the stamped message id, got %q found=%v", messageId, found)
    }
}
