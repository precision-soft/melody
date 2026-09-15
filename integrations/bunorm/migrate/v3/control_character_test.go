package migrate

import (
    "strings"
    "testing"
    "unicode/utf8"
)

func TestEscapeControlCharactersRendersEveryControlCharacterVisibly(t *testing.T) {
    escaped := escapeControlCharacters("before\r\n\tafter\x00\x1b\x7fend", false)

    if `before\r\n\tafter\x00\x1b\x7fend` != escaped {
        t.Fatalf("expected every control character in its visible spelling, got %q", escaped)
    }
}

func TestEscapeControlCharactersSpellsTheNamedThreeByNameAndTheRestInHexadecimal(t *testing.T) {
    for value, expected := range map[string]string{
        "\n":   `\n`,
        "\r":   `\r`,
        "\t":   `\t`,
        "\x00": `\x00`,
        "\x0b": `\x0b`,
        "\x1f": `\x1f`,
        "\x7f": `\x7f`,
    } {
        if expected != escapeControlCharacters(value, false) {
            t.Fatalf("expected %q to render as %s, got %s", value, expected, escapeControlCharacters(value, false))
        }
    }
}

func TestEscapeControlCharactersKeepsTheNewlineWhenAskedAndEscapesTheRest(t *testing.T) {
    escaped := escapeControlCharacters("select 1\nfrom dual\r\tend", true)

    if "select 1\nfrom dual"+`\r\t`+"end" != escaped {
        t.Fatalf("expected the newline kept and the rest escaped, got %q", escaped)
    }

    if false == strings.Contains(escaped, "\n") {
        t.Fatal("the multi-line form must keep the real line break it exists for")
    }
}

func TestEscapeControlCharactersAnswersAnUntouchedValueUnchanged(t *testing.T) {
    plain := "melody_example_v1 on db.internal"

    if plain != escapeControlCharacters(plain, false) {
        t.Fatalf("expected an untouched value to be answered as given, got %q", escapeControlCharacters(plain, false))
    }

    if true == containsControlCharacter(plain, false) {
        t.Fatal("a value with no control character must not be reported as carrying one")
    }
}

func TestIsEscapedControlRuneReadsTheNewlineByTheModeAndTheRestAlike(t *testing.T) {
    if false == isEscapedControlRune('\n', false) {
        t.Fatal("the single-line form must escape the newline")
    }

    if true == isEscapedControlRune('\n', true) {
        t.Fatal("the multi-line form must keep the newline")
    }

    for _, keepNewline := range []bool{false, true} {
        if false == isEscapedControlRune('\x7f', keepNewline) {
            t.Fatal("DEL must be escaped in both forms")
        }

        if true == isEscapedControlRune('a', keepNewline) {
            t.Fatal("an ordinary character must never be escaped")
        }

        if true == isEscapedControlRune('é', keepNewline) {
            t.Fatal("a rune above the control range must never be escaped")
        }
    }
}

func TestEscapeControlCharactersEscapesTheWholeC1Block(t *testing.T) {
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
        escaped := escapeControlCharacters(currentCase.value, false)

        if currentCase.expected != escaped {
            t.Fatalf("%s: expected %q, got %q", currentCase.name, currentCase.expected, escaped)
        }
    }

    for _, neighbour := range []string{"a~b", "a\u00a0b", "a\u00e9b"} {
        if neighbour != escapeControlCharacters(neighbour, false) {
            t.Fatalf("expected a rune outside the block to pass through unchanged, got %q", escapeControlCharacters(neighbour, false))
        }
    }
}

func TestEscapeControlCharactersEscapesTheTwoUnicodeLineSeparators(t *testing.T) {
    for _, currentCase := range []struct {
        name     string
        value    string
        expected string
    }{
        {"LINE SEPARATOR", "a\u2028b", `a\u2028b`},
        {"PARAGRAPH SEPARATOR", "a\u2029b", `a\u2029b`},
    } {
        escaped := escapeControlCharacters(currentCase.value, false)

        if currentCase.expected != escaped {
            t.Fatalf("%s: expected %q, got %q", currentCase.name, currentCase.expected, escaped)
        }
    }
}

func TestEscapeControlCharactersSpellsARuneAboveOneByteInFourDigits(t *testing.T) {
    escaped := escapeControlCharacters("a\u2028b", false)

    if true == strings.Contains(escaped, `\x20`) {
        t.Fatalf("the separator was spelled as a space followed by two digits: %q", escaped)
    }

    if `a\u2028b` != escaped {
        t.Fatalf("expected the four-digit spelling, got %q", escaped)
    }
}

func TestEscapeControlCharactersNoUnicodeLineBoundarySurvives(t *testing.T) {
    boundaries := []rune{'\n', '\v', '\f', '\r', '\x1c', '\x1d', '\x1e', '\u0085', '\u2028', '\u2029'}

    var builder strings.Builder
    for _, boundary := range boundaries {
        builder.WriteString("record")
        builder.WriteRune(boundary)
    }

    escaped := escapeControlCharacters(builder.String(), false)

    for _, boundary := range boundaries {
        if true == strings.ContainsRune(escaped, boundary) {
            t.Fatalf("the record boundary %U survived the escaping: %q", boundary, escaped)
        }
    }
}

func TestEscapeControlCharactersTheEarlyReturnAgreesWithTheLoop(t *testing.T) {
    for _, currentCase := range []struct {
        value    string
        expected string
    }{
        {"\u0085", `\x85`},
        {"\u009b", `\x9b`},
        {"\u2028", `\u2028`},
        {"\u2029", `\u2029`},
    } {
        escaped := escapeControlCharacters(currentCase.value, false)

        if currentCase.value == escaped {
            t.Fatalf("the early return handed back a value the loop escapes: %q", escaped)
        }

        if currentCase.expected != escaped {
            t.Fatalf("expected %q, got %q", currentCase.expected, escaped)
        }
    }
}

func TestEscapeControlCharactersKeepsOnlyTheNewlineAmongTheRecordBoundaries(t *testing.T) {
    escaped := escapeControlCharacters("one\u0085two\u2028three\nfour", true)

    if `one\x85two\u2028three`+"\nfour" != escaped {
        t.Fatalf("unexpected escaping: %q", escaped)
    }

    if false == strings.Contains(escaped, "\n") {
        t.Fatal("the multi-line form must keep the real line break it exists for")
    }
}

func TestEscapeControlCharactersEscapesARawByteThatIsNotValidUtf8(t *testing.T) {
    for _, currentCase := range []struct {
        name     string
        value    string
        expected string
    }{
        {"CSI as a raw byte", "user\x9b31mX", `user\x9b31mX`},
        {"NEL as a raw byte", "line one\x85line two", `line one\x85line two`},
        {"a raw byte outside the C1 block", "x\xffy", `x\xffy`},
        {"a truncated multi-byte sequence", "caf\xc3", `caf\xc3`},
    } {
        escaped := escapeControlCharacters(currentCase.value, false)

        if currentCase.expected != escaped {
            t.Fatalf("%s: expected %q, got %q", currentCase.name, currentCase.expected, escaped)
        }

        if false == utf8.ValidString(escaped) {
            t.Fatalf("%s: expected valid UTF-8 out of the escaping, got %q", currentCase.name, escaped)
        }
    }
}

func TestEscapeControlCharactersDoesNotReplaceARawByteWhenAnotherCharacterForcesTheRewrite(t *testing.T) {
    escaped := escapeControlCharacters("a\x9bb\n", false)

    if `a\x9bb\n` != escaped {
        t.Fatalf("expected the raw byte spelled beside the newline, got %q", escaped)
    }

    if true == strings.ContainsRune(escaped, utf8.RuneError) {
        t.Fatalf("the raw byte was replaced by U+FFFD: %q", escaped)
    }
}

func TestEscapeControlCharactersLeavesAGenuineReplacementRuneUntouched(t *testing.T) {
    if "a\xef\xbf\xbdb" != escapeControlCharacters("a\xef\xbf\xbdb", false) {
        t.Fatalf("expected a genuine U+FFFD to pass through unchanged, got %q", escapeControlCharacters("a\xef\xbf\xbdb", false))
    }

    if "a\xef\xbf\xbdb"+`\x9b` != escapeControlCharacters("a\xef\xbf\xbdb\x9b", false) {
        t.Fatalf("expected the genuine U+FFFD kept and the raw byte spelled, got %q", escapeControlCharacters("a\xef\xbf\xbdb\x9b", false))
    }
}

func TestContainsControlCharacterSeesARawByte(t *testing.T) {
    if false == containsControlCharacter("\x9b", false) {
        t.Fatal("a raw byte must be reported as a control character, or the early return hands it back as sent")
    }

    if `\x9b` != escapeControlCharacters("\x9b", false) {
        t.Fatalf("the early return handed back the raw byte: %q", escapeControlCharacters("\x9b", false))
    }
}

func TestEscapeControlCharactersKeepsTheNewlineBesideARawByte(t *testing.T) {
    escaped := escapeControlCharacters("a\x9bb\nc", true)

    if `a\x9bb`+"\nc" != escaped {
        t.Fatalf("expected the raw byte spelled and the line break kept, got %q", escaped)
    }
}
