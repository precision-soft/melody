package output

import (
    "testing"
)

func TestEnvelope_AddWarningAccumulatesInTheOrderTheyWereRaised(t *testing.T) {
    envelope := Envelope{}

    envelope.AddWarning("first.code", "the first warning", map[string]any{"index": 1})
    envelope.AddWarning("second.code", "the second warning", nil)

    if 2 != len(envelope.Warnings) {
        t.Fatalf("expected both warnings to be kept, got %v", envelope.Warnings)
    }

    if "first.code" != envelope.Warnings[0].Code || "second.code" != envelope.Warnings[1].Code {
        t.Fatalf("expected the warnings in the order they were raised, got %v", envelope.Warnings)
    }

    if "the first warning" != envelope.Warnings[0].Message {
        t.Fatalf("unexpected message: %q", envelope.Warnings[0].Message)
    }

    if 1 != envelope.Warnings[0].Details["index"] {
        t.Fatalf("expected the details to be carried, got %#v", envelope.Warnings[0].Details)
    }
}

func TestEnvelope_SetErrorReplacesTheSingleSlot(t *testing.T) {
    envelope := Envelope{}

    if nil != envelope.Error {
        t.Fatalf("expected a fresh envelope to carry no error")
    }

    envelope.SetError("first.code", "the first failure", nil, nil)
    envelope.SetError(
        "second.code",
        "the second failure",
        map[string]any{"attempt": 2},
        NewErrorCause("the underlying refusal", map[string]any{"host": "acme.example"}),
    )

    if nil == envelope.Error {
        t.Fatalf("expected the envelope to carry an error")
    }

    if "second.code" != envelope.Error.Code || "the second failure" != envelope.Error.Message {
        t.Fatalf("expected the last failure to win, got %#v", envelope.Error)
    }

    if 2 != envelope.Error.Details["attempt"] {
        t.Fatalf("expected the details to be carried, got %#v", envelope.Error.Details)
    }

    if nil == envelope.Error.Cause || "the underlying refusal" != envelope.Error.Cause.Message {
        t.Fatalf("expected the cause to be carried, got %#v", envelope.Error.Cause)
    }
}
