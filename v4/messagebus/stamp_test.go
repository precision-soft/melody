package messagebus

import (
    "testing"
    "time"

    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

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
        {stamp: HeadersStamp{}, expected: "headers"},
    } {
        if probe.expected != probe.stamp.StampName() {
            t.Fatalf("%T: expected %q, got %q", probe.stamp, probe.expected, probe.stamp.StampName())
        }
    }
}

func TestLastStampOfType_AnswersTheMostRecentStampOfItsType(t *testing.T) {
    envelopeInstance := NewEnvelope(
        "payload",
        []messagebuscontract.Stamp{
            RedeliveryStamp{Count: 1},
            SentStamp{TransportName: "amqp"},
            RedeliveryStamp{Count: 3},
        },
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
    stamp, found := LastStampOfType[DelayStamp](NewEnvelope("payload", []messagebuscontract.Stamp{SentStamp{TransportName: "amqp"}}))
    if true == found {
        t.Fatalf("expected no delay stamp, got %v", stamp)
    }

    if (DelayStamp{}) != stamp {
        t.Fatalf("expected the zero stamp when absent, got %v", stamp)
    }
}

func TestStampCounters_ReadZeroOnAnUnstampedEnvelope(t *testing.T) {
    bare := NewEnvelope("payload", nil)

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
    stamped := NewEnvelope("payload", nil).
        WithStamps([]messagebuscontract.Stamp{RedeliveryStamp{Count: 1}, DeadLetterAttemptStamp{Count: 1}}).
        WithStamps([]messagebuscontract.Stamp{RedeliveryStamp{Count: 2}, DeadLetterAttemptStamp{Count: 4}, MessageIdStamp{MessageId: "order-1"}, DelayStamp{Delay: time.Second}})

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

func TestHeaders_MergesEveryStampTheLaterWinning(t *testing.T) {
    first := map[string]string{"request-id": "a", "tenant": "north"}
    envelopeInstance := NewEnvelope(
        "payload",
        []messagebuscontract.Stamp{
            HeadersStamp{Headers: first},
            SentStamp{TransportName: "amqp"},
            HeadersStamp{Headers: map[string]string{"request-id": "b"}},
        },
    )

    headers := Headers(envelopeInstance)
    if 2 != len(headers) || "b" != headers["request-id"] || "north" != headers["tenant"] {
        t.Fatalf("expected the later stamp to win request-id over the merged map, got %v", headers)
    }

    headers["tenant"] = "south"
    if "north" != first["tenant"] || "north" != Headers(envelopeInstance)["tenant"] {
        t.Fatalf("expected a fresh map, an edit reached a stamp")
    }

    empty := Headers(NewEnvelope("payload", nil))
    if nil == empty || 0 != len(empty) {
        t.Fatalf("expected an empty non-nil map for an envelope with no headers stamp, got %#v", empty)
    }
}
