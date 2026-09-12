package security

import (
    "crypto/sha256"
    "encoding/hex"
    "strings"
    "testing"
    "time"
)

/* Two hashes of one password differing is the property the whole change was made for: the seeded rows carry salted hashes, so equality lives in PasswordMatches and nowhere else. */
func TestHashPasswordSaltsEveryCall(t *testing.T) {
    first, firstErr := HashPassword("editor")
    if nil != firstErr {
        t.Fatalf("hash the password: %v", firstErr)
    }

    second, secondErr := HashPassword("editor")
    if nil != secondErr {
        t.Fatalf("hash the password again: %v", secondErr)
    }

    if first == second {
        t.Fatalf("two hashes of the same password are identical, so the hash is not salted")
    }

    if false == PasswordMatches(first, "editor") || false == PasswordMatches(second, "editor") {
        t.Fatalf("a fresh hash refused the password that produced it")
    }
}

func TestPasswordMatchesRefusesAWrongPassword(t *testing.T) {
    passwordHash := MustHashPassword("admin")

    if true == PasswordMatches(passwordHash, "admin-but-wrong") {
        t.Fatalf("a wrong password was accepted")
    }

    if true == PasswordMatches(passwordHash, "") {
        t.Fatalf("an empty password was accepted")
    }
}

func TestPasswordMatchesRefusesAMalformedHash(t *testing.T) {
    if true == PasswordMatches("not-a-bcrypt-hash", "user") {
        t.Fatalf("a malformed stored hash was accepted")
    }

    if true == PasswordMatches("", "user") {
        t.Fatalf("an empty stored hash was accepted")
    }
}

/* bcrypt refuses input past 72 bytes instead of truncating silently, and the two doors answer it differently on purpose: the handler path gets an error to present, the seeding path has no caller to answer and panics. */
func TestHashPasswordRefusesAPasswordOverTheBcryptLimit(t *testing.T) {
    oversized := strings.Repeat("a", 73)

    _, hashErr := HashPassword(oversized)
    if nil == hashErr {
        t.Fatalf("a 73 byte password was hashed")
    }

    defer func() {
        if nil == recover() {
            t.Fatalf("MustHashPassword did not panic on a 73 byte password")
        }
    }()

    MustHashPassword(oversized)
}

/* the ceiling is bytes, not characters: nineteen four-byte runes are over it while seventy-two ascii letters sit exactly on it, which is why the doors validate len() against the constant */
func TestPasswordMaximumBytesIsTheExactBcryptCeiling(t *testing.T) {
    if _, hashErr := HashPassword(strings.Repeat("a", PasswordMaximumBytes)); nil != hashErr {
        t.Fatalf("a password exactly on the ceiling must hash, got %v", hashErr)
    }

    multiByte := strings.Repeat("🔒", 19)
    if PasswordMaximumBytes >= len(multiByte) {
        t.Fatalf("the probe must exceed the ceiling in bytes, got %d", len(multiByte))
    }

    if _, hashErr := HashPassword(multiByte); nil == hashErr {
        t.Fatal("nineteen four-byte runes are over the byte ceiling and must be refused")
    }
}

/* a value bcrypt cannot read at all: an unsalted sha256 rendered as 64 hex characters stands in for any column this application did not write — truncated, edited by hand, or filled by another tool. What matters is only that bcrypt refuses it on the prefix, before deriving a key. */
func storedValueBcryptCannotRead(plaintextPassword string) string {
    digest := sha256.Sum256([]byte(plaintextPassword))

    return hex.EncodeToString(digest[:])
}

/* the window is measured against the cost it separates rather than estimated. bcrypt at the default cost spends about 50ms on a comparison it actually performs; a stored value it cannot read at all was refused in 228ns, five orders of magnitude below that. A floor of five milliseconds sits far above the refusal that does no work and far below the one that does, so neither a loaded machine nor a fast one moves the verdict. */
const equalizedRefusalFloor = 5 * time.Millisecond

/* a refusal bcrypt reaches without deriving a key — a stored value that is not one of its digests — must still cost what a real comparison costs. Unequalized it answered 227.477 times faster than the dummy comparison an absent username pays, so response time told an attacker which accounts hold a value this door cannot use: not merely that a username exists, but that its credential is one this application will refuse whatever is typed. The assertion is on the TIME, because the returned value was already correct while the oracle was open. */
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

/* DummyPasswordMatch is what an absent username pays, and it is the yardstick the refusal above is equalized against; it runs one comparison against a digest it can read, so the equalizing branch must not fire for it. */
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
