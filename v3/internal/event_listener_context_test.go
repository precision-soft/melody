package internal

import (
    "testing"
)

func TestNewEventListenerContext_CarriesTheSixFieldsARecordIsWrittenFrom(t *testing.T) {
    context := NewEventListenerContext(
        "catalog.product.written",
        "*event.ProductWritten",
        "catalog.journal.listener",
        "*listener.CatalogJournal",
        128,
        17,
    )

    for key, expected := range map[string]any{
        "eventName":        "catalog.product.written",
        "eventType":        "*event.ProductWritten",
        "listenerName":     "catalog.journal.listener",
        "listenerType":     "*listener.CatalogJournal",
        "listenerPriority": 128,
        "durationMs":       int64(17),
    } {
        if expected != context[key] {
            t.Fatalf("expected %q to carry %#v, got %#v", key, expected, context[key])
        }
    }

    if 6 != len(context) {
        t.Fatalf("expected exactly the six declared fields, got %v", context)
    }
}

func TestNewEventListenerPanicContext_CopiesTheBaseRatherThanWritingIntoIt(t *testing.T) {
    baseContext := NewEventListenerContext("e", "*E", "l", "*L", 0, 1)

    panicContext := NewEventListenerPanicContext(baseContext, "boom", "string", "goroutine 1 [running]")

    if "boom" != panicContext["panicValue"] || "string" != panicContext["panicType"] {
        t.Fatalf("expected the panic value and type to be recorded, got %v", panicContext)
    }

    if "goroutine 1 [running]" != panicContext["panicStack"] {
        t.Fatalf("expected the stack to be recorded, got %#v", panicContext["panicStack"])
    }

    if _, leaked := baseContext["panicValue"]; true == leaked {
        t.Fatalf("expected the base context to be left untouched, got %v", baseContext)
    }

    if 6 != len(baseContext) {
        t.Fatalf("expected the base context to keep its six fields, got %v", baseContext)
    }

    if "e" != panicContext["eventName"] {
        t.Fatalf("expected the base fields to survive the copy, got %v", panicContext)
    }
}
