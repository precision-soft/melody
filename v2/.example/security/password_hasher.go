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

/* PasswordMatches reports whether the plaintext password produced the stored hash; bcrypt compares in constant time. A stored value that is not a bcrypt digest is refused far faster than a real comparison, which would reveal the accounts whose column holds one, so a refusal bcrypt reached without working spends the comparison it skipped, as DummyPasswordMatch does for an absent user. */
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

/* dummyPasswordHash is one bcrypt hash at the default cost, computed once at load, the material DummyPasswordMatch compares against. */
var dummyPasswordHash = MustHashPassword("melody-example-absent-user-timing-equalizer")

/* DummyPasswordMatch runs a full bcrypt comparison against a fixed hash no account carries. A login door that could not find the user calls it and discards the answer, so an absent username costs what a wrong password costs and response time does not reveal which usernames exist. */
func DummyPasswordMatch(plaintextPassword string) bool {
    return PasswordMatches(dummyPasswordHash, plaintextPassword)
}
