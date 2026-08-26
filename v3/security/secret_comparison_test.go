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

    /* the length mismatch is the case the digest form exists for: compared directly, subtle.ConstantTimeCompare answers it without reading a byte, and the timing said so */
    if true == constantTimeSecretEquals("expected-key", "short") {
        t.Fatalf("expected the shorter guess to compare unequal")
    }

    if true == constantTimeSecretEquals("expected-key", "") {
        t.Fatalf("expected the empty guess to compare unequal")
    }
}
