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

/* PasswordMatches verifies a password with bcrypt. Malformed hashes and overlong passwords pay a dummy default-cost comparison before refusal; valid hashes use their stored cost. This does not make the complete login path constant-time. */
func PasswordMatches(passwordHash string, plaintextPassword string) bool {
    compareErr := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(plaintextPassword))
    if nil == compareErr {
        return true
    }

    if false == errors.Is(compareErr, bcrypt.ErrMismatchedHashAndPassword) {
        _ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(plaintextPassword))
    }

    return false
}

var dummyPasswordHash = MustHashPassword("melody-example-absent-user-timing-equalizer")

/* DummyPasswordMatch performs a default-cost bcrypt comparison and returns false. It reduces the timing difference for absent users; existing hashes with different costs still take different amounts of work. */
func DummyPasswordMatch(plaintextPassword string) bool {
    return PasswordMatches(dummyPasswordHash, plaintextPassword)
}
