package security

import (
    "strings"
    "testing"
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
