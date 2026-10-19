package security

import (
    "errors"
    "fmt"

    melodyexception "github.com/precision-soft/melody/v3/exception"
    "golang.org/x/crypto/bcrypt"
)

/* PasswordMaximumBytes is bcrypt's input ceiling, counted in bytes rather than characters, so the doors that admit a password validate against it and answer a 400 instead of letting the hasher fail. */
const PasswordMaximumBytes = 72

/* PasswordTooLongMessage is the refusal both admitting doors answer for a password over the ceiling, kept beside the number it quotes. */
const PasswordTooLongMessage = "password must not exceed 72 bytes"

/* HashPassword answers the bcrypt hash of the given plaintext password. Each call salts anew, so two hashes of the same password differ; equality is decided by PasswordMatches, never by comparing hashes. */
func HashPassword(plaintextPassword string) (string, error) {
    passwordHash, hashErr := bcrypt.GenerateFromPassword([]byte(plaintextPassword), bcrypt.DefaultCost)
    if nil != hashErr {
        return "", fmt.Errorf("failed to hash password: %w", hashErr)
    }

    return string(passwordHash), nil
}

/* MustHashPassword is the seeding door: at the default cost bcrypt fails only on a password over 72 bytes, which in a seed is a mistake of declaration. */
func MustHashPassword(plaintextPassword string) string {
    passwordHash, hashErr := HashPassword(plaintextPassword)
    if nil != hashErr {
        melodyexception.Panic(melodyexception.FromError(hashErr))
    }

    return passwordHash
}

/* PasswordMatches reports whether the plaintext password produced the stored hash; bcrypt compares in constant time. A stored value that is not a bcrypt digest is refused far faster than a real comparison, so such a refusal spends the comparison it skipped: otherwise response time would name every account whose column holds an unusable value. */
func PasswordMatches(passwordHash string, plaintextPassword string) bool {
    compareErr := comparePasswordHash([]byte(passwordHash), []byte(plaintextPassword))
    if nil == compareErr {
        return true
    }

    /* a mismatch is the one refusal bcrypt pays for in full; every other one is a stored value it could not use */
    if false == errors.Is(compareErr, bcrypt.ErrMismatchedHashAndPassword) {
        _ = comparePasswordHash([]byte(dummyPasswordHash), []byte(plaintextPassword))
    }

    return false
}

/* comparePasswordHash is the one comparison every refusal and every match goes through; the tests count its calls, pinning the equalizing branch on what it does rather than how long it takes. */
var comparePasswordHash = bcrypt.CompareHashAndPassword

/* dummyPasswordHash is one bcrypt hash at the default cost, computed once at load, the material DummyPasswordMatch compares against. */
var dummyPasswordHash = MustHashPassword("melody-example-absent-user-timing-equalizer")

/* DummyPasswordMatch runs a full bcrypt comparison against a fixed hash no account carries. A login door that could not find the user calls it and discards the answer, so an absent username costs what a wrong password costs and response time does not reveal which usernames exist. */
func DummyPasswordMatch(plaintextPassword string) bool {
    return PasswordMatches(dummyPasswordHash, plaintextPassword)
}
