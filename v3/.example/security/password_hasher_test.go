package security

import (
    "testing"
    "time"
)

func TestPasswordMatches_SpendsTheComparisonOnAStoredValueBcryptCannotRead(t *testing.T) {
    startedAt := time.Now()
    matched := PasswordMatches(storedValueBcryptCannotRead("admin"), "admin")
    refusalCost := time.Since(startedAt)

    if true == matched {
        t.Fatalf("expected a stored value that is not a bcrypt digest to be refused")
    }

    if equalizedRefusalFloor > refusalCost {
        t.Fatalf(
            "expected the refusal to spend the comparison bcrypt skipped, but it answered in %v, under the %v floor",
            refusalCost,
            equalizedRefusalFloor,
        )
    }
}

func TestPasswordMatches_RefusesAWrongPasswordAgainstARealDigest(t *testing.T) {
    passwordHash := MustHashPassword("admin")

    if true == PasswordMatches(passwordHash, "not-the-password") {
        t.Fatalf("expected a wrong password to be refused")
    }

    if false == PasswordMatches(passwordHash, "admin") {
        t.Fatalf("expected the password that produced the digest to be accepted")
    }
}

func TestDummyPasswordMatch_AlwaysRefusesAndPaysOneComparison(t *testing.T) {
    startedAt := time.Now()
    matched := DummyPasswordMatch("anything at all")
    dummyCost := time.Since(startedAt)

    if true == matched {
        t.Fatalf("expected the equalizing comparison to always refuse")
    }

    if equalizedRefusalFloor > dummyCost {
        t.Fatalf("expected the equalizing comparison to cost a real one, got %v", dummyCost)
    }
}
