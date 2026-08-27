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

/* the C1 block is the second control block, and a terminal decoding UTF-8 obeys it without an ESC ever appearing: U+009B is the control sequence introducer that "\x1b[" abbreviates, so one rune repaints a line the escaping was meant to make inert. The whole 0x80…0x9f range is escaped and both ends are entered, because a range guard fails at its ends first. */
func TestEscapeControlCharacters_EscapesTheWholeC1Block(t *testing.T) {
    for _, currentCase := range []struct {
        name     string
        value    string
        expected string
    }{
        {"PAD, the low end of the block", "a\u0080b", `a\x80b`},
        {"NEL, a record end for a unicode line reader", "a\u0085b", `a\x85b`},
        {"CSI, the single-byte control sequence introducer", "a\u009bb", `a\x9bb`},
        {"OSC, the operating system command introducer", "a\u009db", `a\x9db`},
        {"APC, the high end of the block", "a\u009fb", `a\x9fb`},
    } {
        escaped := EscapeControlCharacters(currentCase.value)

        if currentCase.expected != escaped {
            t.Fatalf("%s: expected %q, got %q", currentCase.name, currentCase.expected, escaped)
        }
    }

    /* the runes on either side of the block are ordinary text and must survive as themselves, or the guard escapes what it was never asked to */
    for _, neighbour := range []string{"a~b", "a\u00a0b", "a\u00e9b"} {
        if neighbour != EscapeControlCharacters(neighbour) {
            t.Fatalf("expected a rune outside the block to pass through unchanged, got %q", EscapeControlCharacters(neighbour))
        }
    }
}

/* LINE SEPARATOR and PARAGRAPH SEPARATOR are the only runes outside the control blocks that a unicode line splitter reads as a record boundary, so a log line carrying one is read downstream as two records — the half of the harm that does not depend on which terminal is attached. */
func TestEscapeControlCharacters_EscapesTheTwoUnicodeLineSeparators(t *testing.T) {
    for _, currentCase := range []struct {
        name     string
        value    string
        expected string
    }{
        {"LINE SEPARATOR", "a\u2028b", `a\u2028b`},
        {"PARAGRAPH SEPARATOR", "a\u2029b", `a\u2029b`},
    } {
        escaped := EscapeControlCharacters(currentCase.value)

        if currentCase.expected != escaped {
            t.Fatalf("%s: expected %q, got %q", currentCase.name, currentCase.expected, escaped)
        }
    }
}

/* a rune above one byte cannot take the \xNN spelling: \x2028 reads as \x20 followed by the digits 28, which is a space and not a separator, so the four-digit form is what keeps the two apart in a log a human reads. */
func TestEscapeControlCharacters_SpellsARuneAboveOneByteInFourDigits(t *testing.T) {
    escaped := EscapeControlCharacters("a\u2028b")

    if true == strings.Contains(escaped, `\x20`) {
        t.Fatalf("the separator was spelled as a space followed by two digits: %q", escaped)
    }

    if `a\u2028b` != escaped {
        t.Fatalf("expected the four-digit spelling, got %q", escaped)
    }
}

/* every rune a unicode line splitter counts as a record boundary has to leave the value escaped, or one record is read downstream as two. The set is entered whole — the C0 breaks, the file, group and record separators, NEL and the two unicode separators — because the last three are exactly the ones a C0-and-DEL predicate passes through. */
func TestEscapeControlCharacters_NoUnicodeLineBoundarySurvives(t *testing.T) {
    boundaries := []rune{'\n', '\v', '\f', '\r', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029'}

    var builder strings.Builder
    for _, boundary := range boundaries {
        builder.WriteString("record")
        builder.WriteRune(boundary)
    }

    escaped := EscapeControlCharacters(builder.String())

    for _, boundary := range boundaries {
        if true == strings.ContainsRune(escaped, boundary) {
            t.Fatalf("the record boundary %U survived the escaping: %q", boundary, escaped)
        }
    }
}

/* the early return answers a value it finds nothing to escape in with the value itself, so it has to agree with the loop rune for rune: a value made of one newly-escaped rune and nothing else is the shape that tells the two apart. */
func TestEscapeControlCharacters_TheEarlyReturnAgreesWithTheLoop(t *testing.T) {
    for _, currentCase := range []struct {
        value    string
        expected string
    }{
        {"\u0085", `\x85`},
        {"\u009b", `\x9b`},
        {"\u2028", `\u2028`},
        {"\u2029", `\u2029`},
    } {
        escaped := EscapeControlCharacters(currentCase.value)

        if currentCase.value == escaped {
            t.Fatalf("the early return handed back a value the loop escapes: %q", escaped)
        }

        if currentCase.expected != escaped {
            t.Fatalf("expected %q, got %q", currentCase.expected, escaped)
        }
    }
}

/* the cell form keeps the newline the consumer renders on purpose, and nothing else: NEL and the two unicode separators are record boundaries the table never asked for, and they stay escaped in both forms. */
func TestEscapeControlCharactersKeepingNewlines_KeepsOnlyTheNewline(t *testing.T) {
    escaped := EscapeControlCharactersKeepingNewlines("one\u0085two\u2028three\nfour")

    if `one\x85two\u2028three`+"\nfour" != escaped {
        t.Fatalf("unexpected escaping: %q", escaped)
    }

    if false == strings.Contains(escaped, "\n") {
        t.Fatal("the cell form must keep the real line break it exists for")
    }
}
