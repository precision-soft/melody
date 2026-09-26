package security

import (
    "crypto/sha256"
    "encoding/hex"
    "testing"
    "time"
)

/* a value bcrypt cannot read at all: an unsalted sha256 rendered as 64 hex characters stands in for any column this application did not write — truncated, edited by hand, or filled by another tool. What matters is only that bcrypt refuses it on the prefix, before deriving a key. */
func storedValueBcryptCannotRead(plaintextPassword string) string {
    digest := sha256.Sum256([]byte(plaintextPassword))

    return hex.EncodeToString(digest[:])
}

/* the floor sits between the two costs it separates: bcrypt at the default cost spends tens of milliseconds on a comparison it performs, and a stored value it cannot read at all is refused in well under a microsecond. Five milliseconds is far above the one and far below the other, so neither a loaded machine nor a fast one moves the verdict. */
const equalizedRefusalFloor = 5 * time.Millisecond

/* a refusal bcrypt reaches without deriving a key, a stored value that is not one of its digests, must still cost what a real comparison costs: otherwise response time tells an attacker which accounts hold a value this door cannot use, a credential this application will refuse whatever is typed. The assertion is on the time, because the returned value is correct either way. */
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

/* the sister case, which is what keeps the assertion above from passing over a door that sleeps on every
   refusal: a wrong password against a real digest is the refusal bcrypt pays for in full, and it must not
   pay for it twice. */
func TestPasswordMatches_RefusesAWrongPasswordAgainstARealDigest(t *testing.T) {
    passwordHash := MustHashPassword("admin")

    if true == PasswordMatches(passwordHash, "not-the-password") {
        t.Fatalf("expected a wrong password to be refused")
    }

    if false == PasswordMatches(passwordHash, "admin") {
        t.Fatalf("expected the password that produced the digest to be accepted")
    }
}

/* countingComparisons routes the comparison through a counter for the duration of the test and hands the real bcrypt back afterwards. */
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

/* the equalizing branch fires for a stored value bcrypt cannot read and for nothing else: a wrong password against a real digest pays one comparison, a value bcrypt refuses on the prefix pays two — its own refusal plus the one it skipped. The floors above bound the time; this pins the count, which no loaded machine can move. */
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

/* DummyPasswordMatch is what an absent username pays, and it is the yardstick the refusal above is
   equalized against; it runs one comparison against a digest it can read, so the equalizing branch must not
   fire for it. */
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
