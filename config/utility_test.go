package config

import (
    "testing"
)

func TestIntWithDefault_DefaultOnlyOnAbsence(t *testing.T) {
    if 10 != IntWithDefault(nil, 10) {
        t.Fatalf("expected the default for a nil parameter")
    }

    var typedNilParameter *Parameter
    if 10 != IntWithDefault(typedNilParameter, 10) {
        t.Fatalf("expected the default for a typed-nil parameter")
    }

    presentParameter := NewParameter("APP_POOL", "42", "42", false)
    if 42 != IntWithDefault(presentParameter, 10) {
        t.Fatalf("expected the parsed value for a present parameter")
    }

    malformedParameter := NewParameter("APP_POOL", "1O0", "1O0", false)

    defer func() {
        if recoveredValue := recover(); nil == recoveredValue {
            t.Fatalf("expected the malformed value to panic instead of becoming the default")
        }
    }()

    _ = IntWithDefault(malformedParameter, 10)
}
