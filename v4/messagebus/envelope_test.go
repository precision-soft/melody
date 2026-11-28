package messagebus

import (
    "testing"

    messagebuscontract "github.com/precision-soft/melody/v4/messagebus/contract"
)

func TestNewEnvelope_CarriesTheMessageAndItsStamps(t *testing.T) {
    envelopeInstance := NewEnvelope("payload", []messagebuscontract.Stamp{SentStamp{TransportName: "amqp"}})

    if "payload" != envelopeInstance.Message() {
        t.Fatalf("expected the message to be carried, got %v", envelopeInstance.Message())
    }

    if 1 != len(envelopeInstance.Stamps()) {
        t.Fatalf("expected one stamp, got %v", envelopeInstance.Stamps())
    }
}

func TestEnsureEnvelope_LeavesAnEnvelopeAlone(t *testing.T) {
    original := NewEnvelope("payload", []messagebuscontract.Stamp{SentStamp{TransportName: "amqp"}})

    if original != EnsureEnvelope(original) {
        t.Fatal("expected an envelope to be handed back as it is")
    }

    wrapped := EnsureEnvelope("payload")
    if "payload" != wrapped.Message() {
        t.Fatalf("expected a bare message to be wrapped, got %v", wrapped.Message())
    }

    if 0 != len(wrapped.Stamps()) {
        t.Fatalf("expected a freshly wrapped message to carry no stamps, got %v", wrapped.Stamps())
    }
}

func TestEnvelope_WithStampDoesNotMutateTheEnvelopeItWasCalledOn(t *testing.T) {
    original := NewEnvelope("payload", []messagebuscontract.Stamp{SentStamp{TransportName: "amqp"}})

    stamped := original.WithStamps([]messagebuscontract.Stamp{HandledStamp{HandlerName: "handler"}})

    if 1 != len(original.Stamps()) {
        t.Fatalf("expected the original envelope to keep its own stamps, got %v", original.Stamps())
    }

    if 2 != len(stamped.Stamps()) {
        t.Fatalf("expected the stamped envelope to carry both, got %v", stamped.Stamps())
    }

    if "payload" != stamped.Message() {
        t.Fatalf("expected the message to travel with the stamps, got %v", stamped.Message())
    }
}

func TestEnvelope_WithStampKeepsTheStampsInTheOrderTheyWereAdded(t *testing.T) {
    stamped := NewEnvelope("payload", []messagebuscontract.Stamp{RedeliveryStamp{Count: 1}}).WithStamps([]messagebuscontract.Stamp{RedeliveryStamp{Count: 2}})

    stamps := stamped.Stamps()
    if 2 != len(stamps) {
        t.Fatalf("expected two stamps, got %v", stamps)
    }

    if 1 != stamps[0].(RedeliveryStamp).Count || 2 != stamps[1].(RedeliveryStamp).Count {
        t.Fatalf("expected the stamps in the order they were added, got %v", stamps)
    }
}

var _ messagebuscontract.Envelope = NewEnvelope("payload", nil)

func TestNewEnvelope_CopiesTheStamps(t *testing.T) {
    stamps := []messagebuscontract.Stamp{SentStamp{TransportName: "amqp"}}
    envelopeInstance := NewEnvelope("payload", stamps)

    stamps[0] = SentStamp{TransportName: "changed"}
    if "amqp" != envelopeInstance.Stamps()[0].(SentStamp).TransportName {
        t.Fatalf("expected the envelope to keep its own copy of the stamps, got %v", envelopeInstance.Stamps())
    }

    if 0 != len(NewEnvelope("payload", nil).Stamps()) {
        t.Fatalf("expected a nil stamp list to be an envelope with no stamp")
    }
}

func TestEnvelope_WithStampsAppendsInOrder(t *testing.T) {
    envelopeInstance := NewEnvelope("payload", []messagebuscontract.Stamp{SentStamp{TransportName: "a"}}).
        WithStamps([]messagebuscontract.Stamp{SentStamp{TransportName: "b"}, SentStamp{TransportName: "c"}}).
        WithStamps([]messagebuscontract.Stamp{SentStamp{TransportName: "d"}})

    names := ""
    for _, stamp := range envelopeInstance.Stamps() {
        names += stamp.(SentStamp).TransportName
    }
    if "abcd" != names {
        t.Fatalf("expected the stamps in the order they were added, got %q", names)
    }
}
