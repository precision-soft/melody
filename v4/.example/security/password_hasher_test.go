package security

import (
    "crypto/sha256"
    "encoding/hex"
    "testing"
    "time"
)

/* an unsalted sha256 in hex: bcrypt refuses it on the prefix, before deriving a key */
func storedValueBcryptCannotRead(plaintextPassword string) string {
    digest := sha256.Sum256([]byte(plaintextPassword))

    return hex.EncodeToString(digest[:])
}

/* five milliseconds: far above the refusal of an unreadable value, far below a default-cost comparison */
const equalizedRefusalFloor = 5 * time.Millisecond

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

func countingComparisons(t *testing.T) *int {
    t.Helper()

    calls := 0
    previous := comparePasswordHash
    comparePasswordHash = func(passwordHash []byte, plaintextPassword []byte) error {
        calls++

        return previous(passwordHash, plaintextPassword)
    }
    t.Cleanup(func() { comparePasswordHash = previous })

    return &calls
}

func TestPasswordMatches_PaysOneComparisonOnAWrongPasswordAndTwoOnAValueBcryptCannotRead(t *testing.T) {
    calls := countingComparisons(t)
    passwordHash := MustHashPassword("admin")

    if true == PasswordMatches(passwordHash, "not-the-password") {
        t.Fatalf("expected a wrong password to be refused")
    }
    if 1 != *calls {
        t.Fatalf("expected a wrong password to pay exactly one comparison, got %d", *calls)
    }

    *calls = 0
    if true == PasswordMatches(storedValueBcryptCannotRead("admin"), "admin") {
        t.Fatalf("expected a stored value that is not a bcrypt digest to be refused")
    }
    if 2 != *calls {
        t.Fatalf("expected a value bcrypt cannot read to pay the comparison it skipped as well, got %d", *calls)
    }

    *calls = 0
    if false == PasswordMatches(passwordHash, "admin") {
        t.Fatalf("expected the password that produced the digest to be accepted")
    }
    if 1 != *calls {
        t.Fatalf("expected a match to pay exactly one comparison, got %d", *calls)
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
