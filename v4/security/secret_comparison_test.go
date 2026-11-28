package security

import (
    "testing"
)

func TestConstantTimeSecretEquals_AnswersEqualityWhateverTheLengths(t *testing.T) {
    if false == constantTimeSecretEquals("expected-key", "expected-key") {
        t.Fatalf("expected the equal secrets to compare equal")
    }

    if true == constantTimeSecretEquals("expected-key", "expected-kez") {
        t.Fatalf("expected the same-length difference to compare unequal")
    }

    if true == constantTimeSecretEquals("expected-key", "short") {
        t.Fatalf("expected the shorter guess to compare unequal")
    }

    if true == constantTimeSecretEquals("expected-key", "") {
        t.Fatalf("expected the empty guess to compare unequal")
    }

    if true == constantTimeSecretEquals("short", "expected-key-that-is-much-longer") {
        t.Fatalf("expected the longer guess to compare unequal")
    }

    if false == constantTimeSecretEquals("", "") {
        t.Fatalf("expected two empty secrets to compare equal, which is why an empty expected value is refused at construction")
    }
}
