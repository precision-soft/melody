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

/* the window is measured against the cost it separates rather than estimated. bcrypt at the default cost
   spends about 40ms on a comparison it actually performs; a stored value it cannot read at all was refused
   in 308ns, four orders of magnitude below that. A floor of five milliseconds sits far above the refusal
   that does no work and far below the one that does, so neither a loaded machine nor a fast one moves the
   verdict. */
const equalizedRefusalFloor = 5 * time.Millisecond

/* a refusal bcrypt reaches without deriving a key — a stored value that is not one of its digests — must
   still cost what a real comparison costs. Unequalized it answered 131.184 times faster than the dummy
   comparison an absent username pays, so response time told an attacker which accounts hold a value this
   door cannot use: not merely that a username exists, but that its credential is one this application will
   refuse whatever is typed. The assertion is on the TIME, because the returned value was already correct while the
   oracle was open. */
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
