package output

import (
    "testing"
)

/* @info the envelope is what every command answers with, and its two mutators had never been called: warnings ACCUMULATE — a command that gathered three and reported one would hide two — while the error is a single slot, because a command fails once and the last failure is the one that ended it. */
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

/* @info the error slot is REPLACED rather than accumulated, and it starts empty: a command that succeeded must answer with no error at all, because a client reads that slot to decide whether to act on the data beside it. */
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
