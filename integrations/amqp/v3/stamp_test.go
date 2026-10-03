package amqp

import (
    "testing"

    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

/* the stamp name is what separates this stamp from every other one an envelope carries: a drift here makes the acknowledgement stamp invisible to the reader that acks, and nothing in the type system says so */
func TestDeliveryStamp_CarriesItsWireNameAndTheAcknowledgementFacts(t *testing.T) {
    stamp := DeliveryStamp{Tag: 7, Redelivered: true, Generation: 3}

    if "amqp_delivery" != stamp.StampName() {
        t.Fatalf("expected the delivery stamp name, got %q", stamp.StampName())
    }

    if 7 != stamp.Tag || false == stamp.Redelivered || 3 != stamp.Generation {
        t.Fatalf("expected the delivery facts to be carried, got %+v", stamp)
    }
}

/* the stamp is a VALUE: the generation is what makes an ack of a stale connection a no-op, so two stamps that differ only in generation must not compare equal */
func TestDeliveryStamp_ComparesByEveryFieldIncludingTheGeneration(t *testing.T) {
    if (DeliveryStamp{Tag: 7, Generation: 1}) == (DeliveryStamp{Tag: 7, Generation: 2}) {
        t.Fatal("expected the generation to take part in the comparison")
    }

    if (DeliveryStamp{Tag: 7, Generation: 1}) != (DeliveryStamp{Tag: 7, Generation: 1}) {
        t.Fatal("expected two identical stamps to compare equal")
    }
}

var _ messagebuscontract.Stamp = DeliveryStamp{}
