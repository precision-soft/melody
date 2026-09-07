package security

import (
    "errors"
    "fmt"

    "golang.org/x/crypto/bcrypt"
)

/* PasswordMaximumBytes is bcrypt's input ceiling, counted in bytes rather than characters: a password of 19 four-byte runes is over it while 72 ascii letters are exactly on it, so the doors that admit a password validate against this constant and answer a 400 instead of letting the hasher fail. */
const PasswordMaximumBytes = 72

/* HashPassword answers the bcrypt hash of the given plaintext password. Each call salts anew, so two hashes of the same password differ; equality is decided by PasswordMatches, never by comparing hashes. */
func HashPassword(plaintextPassword string) (string, error) {
    passwordHash, hashErr := bcrypt.GenerateFromPassword([]byte(plaintextPassword), bcrypt.DefaultCost)
    if nil != hashErr {
        return "", fmt.Errorf("failed to hash password: %w", hashErr)
    }

    return string(passwordHash), nil
}

/* MustHashPassword is the seeding door: the only error bcrypt can answer at the default cost is a password over 72 bytes, and a seed that long is a mistake of declaration, not a runtime condition. */
func MustHashPassword(plaintextPassword string) string {
    passwordHash, hashErr := HashPassword(plaintextPassword)
    if nil != hashErr {
        panic(hashErr)
    }

    return passwordHash
}

/* PasswordMatches reports whether the plaintext password produced the stored hash. bcrypt compares in constant time internally, so this is the whole credential comparison a caller needs.

   A stored value that is not a bcrypt digest at all is refused before any key is derived, and that refusal is orders of magnitude cheaper than a real comparison: measured here, 228ns against 52ms. Response time would therefore name every account whose column holds such a value — truncated, edited by hand, written by something that is not this application — and those are exactly the accounts this door will refuse whatever is typed, which is the existence oracle DummyPasswordMatch exists to close, inverted. So a refusal bcrypt reached without working spends the comparison it skipped. */
func PasswordMatches(passwordHash string, plaintextPassword string) bool {
    compareErr := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(plaintextPassword))
    if nil == compareErr {
        return true
    }

    /* a mismatch is the one refusal bcrypt pays for in full; every other one — the wrong prefix, a hash too short, an unreadable cost — is a stored value it could not use */
    if false == errors.Is(compareErr, bcrypt.ErrMismatchedHashAndPassword) {
        _ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(plaintextPassword))
    }

    return false
}

/* dummyPasswordHash is one bcrypt hash at the default cost, computed once at load. It is the material DummyPasswordMatch compares against so a login for a username that does not exist spends the same bcrypt time as one whose password is merely wrong. */
var dummyPasswordHash = MustHashPassword("melody-example-absent-user-timing-equalizer")

/* DummyPasswordMatch runs a full bcrypt comparison against a fixed hash and always reports false. A login door that could not find the user calls it so the request pays the same comparison cost a found user's wrong password pays: without it, an absent username returns before any bcrypt work and its faster response is an existence oracle an attacker times to enumerate usernames. */
func DummyPasswordMatch(plaintextPassword string) bool {
    return PasswordMatches(dummyPasswordHash, plaintextPassword)
}
