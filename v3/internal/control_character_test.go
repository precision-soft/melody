package internal

import (
    "strings"
    "testing"
)

func TestEscapeControlCharacters_EscapesEveryControlCharacterVisibly(t *testing.T) {
    escaped := EscapeControlCharacters("a\x1b[2Jb\rc\nd\te\x7ff\x00g")

    if `a\x1b[2Jb\rc\nd\te\x7ff\x00g` != escaped {
        t.Fatalf("unexpected escaping: %q", escaped)
    }

    for _, forbiddenRune := range []rune{'\x1b', '\r', '\n', '\t', '\x7f', '\x00'} {
        if true == strings.ContainsRune(escaped, forbiddenRune) {
            t.Fatalf("expected no raw control character to survive, found %q in %q", forbiddenRune, escaped)
        }
    }
}

func TestEscapeControlCharacters_LeavesACleanValueUntouched(t *testing.T) {
    value := "plain text with unicode — 東京 café"

    if value != EscapeControlCharacters(value) {
        t.Fatalf("expected a value without control characters to pass through unchanged")
    }
}

func TestEscapeControlCharactersKeepingNewlines_KeepsTheLineBreakAndEscapesTheRest(t *testing.T) {
    escaped := EscapeControlCharactersKeepingNewlines("line one\nline two\x1b\r")

    if "line one\nline two\\x1b\\r" != escaped {
        t.Fatalf("unexpected escaping: %q", escaped)
    }
}
