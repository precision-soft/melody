package migrate

import (
    "strings"
    "testing"
    "unicode/utf8"
)

/* text of server origin reaches an operator's terminal through this helper — an error message off the wire, a reported version string, a failed statement — so a control character it carries must arrive as its visible spelling rather than as an instruction the terminal obeys. A carriage return alone repaints the line the previous record wrote; a newline forges a whole record in a captured log. */
func TestEscapeControlCharactersRendersEveryControlCharacterVisibly(t *testing.T) {
    escaped := escapeControlCharacters("before\r\n\tafter\x00\x1b\x7fend", false)

    if `before\r\n\tafter\x00\x1b\x7fend` != escaped {
        t.Fatalf("expected every control character in its visible spelling, got %q", escaped)
    }
}

/* the named three are spelled by name and everything else by its hexadecimal code, so a reader can tell a tab from a vertical tab instead of reading one escape for both */
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

/* the multi-line form is what the query rendering uses, where a real line break is the point: the newline survives and every other control character is still escaped, so a multi-line statement stays readable without becoming a way to forge lines */
func TestEscapeControlCharactersKeepsTheNewlineWhenAskedAndEscapesTheRest(t *testing.T) {
    escaped := escapeControlCharacters("select 1\nfrom dual\r\tend", true)

    if "select 1\nfrom dual"+`\r\t`+"end" != escaped {
        t.Fatalf("expected the newline kept and the rest escaped, got %q", escaped)
    }

    if false == strings.Contains(escaped, "\n") {
        t.Fatal("the multi-line form must keep the real line break it exists for")
    }
}

/* a value with nothing to escape is answered as it was given: the helper runs on every rendered cell, and rebuilding an untouched string for each of them is the cost this guard pins */
func TestEscapeControlCharactersAnswersAnUntouchedValueUnchanged(t *testing.T) {
    plain := "melody_example_v1 on db.internal"

    if plain != escapeControlCharacters(plain, false) {
        t.Fatalf("expected an untouched value to be answered as given, got %q", escapeControlCharacters(plain, false))
    }

    if true == containsControlCharacter(plain, false) {
        t.Fatal("a value with no control character must not be reported as carrying one")
    }
}

/* the newline is the one character whose classification depends on the mode, and it is the whole difference between the two forms */
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

/* the C1 block is the second control block, and a terminal decoding UTF-8 obeys it without an ESC ever appearing: U+009B is the control sequence introducer that "\x1b[" abbreviates, so one rune repaints a line the escaping was meant to make inert. The whole 0x80…0x9f range is escaped and both ends are entered, because a range guard fails at its ends first. */
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

    /* the runes on either side of the block are ordinary text and must survive as themselves, or the guard escapes what it was never asked to */
    for _, neighbour := range []string{"a~b", "a\u00a0b", "a\u00e9b"} {
        if neighbour != escapeControlCharacters(neighbour, false) {
            t.Fatalf("expected a rune outside the block to pass through unchanged, got %q", escapeControlCharacters(neighbour, false))
        }
    }
}

/* LINE SEPARATOR and PARAGRAPH SEPARATOR are the only runes outside the control blocks that a unicode line splitter reads as a record boundary, so a log line carrying one is read downstream as two records — the half of the harm that does not depend on which terminal is attached. */
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

/* a rune above one byte cannot take the \xNN spelling: \x2028 reads as \x20 followed by the digits 28, which is a space and not a separator, so the four-digit form is what keeps the two apart in a log a human reads. */
func TestEscapeControlCharactersSpellsARuneAboveOneByteInFourDigits(t *testing.T) {
    escaped := escapeControlCharacters("a\u2028b", false)

    if true == strings.Contains(escaped, `\x20`) {
        t.Fatalf("the separator was spelled as a space followed by two digits: %q", escaped)
    }

    if `a\u2028b` != escaped {
        t.Fatalf("expected the four-digit spelling, got %q", escaped)
    }
}

/* every rune a unicode line splitter counts as a record boundary has to leave the value escaped, or one record is read downstream as two. The set is entered whole — the C0 breaks, the file, group and record separators, NEL and the two unicode separators — because the last three are exactly the ones a C0-and-DEL predicate passes through. */
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

/* the early return answers a value it finds nothing to escape in with the value itself, so it has to agree with the loop rune for rune: a value made of one newly-escaped rune and nothing else is the shape that tells the two apart. */
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

/* the multi-line form keeps the newline the query rendering exists for, and nothing else: NEL and the two unicode separators are record boundaries the query rendering never asked for, and they stay escaped in both forms. */
func TestEscapeControlCharactersKeepsOnlyTheNewlineAmongTheRecordBoundaries(t *testing.T) {
    escaped := escapeControlCharacters("one\u0085two\u2028three\nfour", true)

    if `one\x85two\u2028three`+"\nfour" != escaped {
        t.Fatalf("unexpected escaping: %q", escaped)
    }

    if false == strings.Contains(escaped, "\n") {
        t.Fatal("the multi-line form must keep the real line break it exists for")
    }
}

/* the server can answer the single-byte C1 introducer raw, and a walk over runes read that byte as U+FFFD, outside every range the guard checks, so the raw spelling went to the terminal as sent while the encoded one was escaped. Every byte that starts no valid sequence is spelled \xNN, and what comes out is valid UTF-8. */
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

/* once another control character forced the rewrite, the raw byte was written out as U+FFFD: the same answer was kept as sent when the byte stood alone and corrupted when a newline stood beside it, and what the server sent was destroyed instead of shown. Neither form may carry the replacement rune. */
func TestEscapeControlCharactersDoesNotReplaceARawByteWhenAnotherCharacterForcesTheRewrite(t *testing.T) {
    escaped := escapeControlCharacters("a\x9bb\n", false)

    if `a\x9bb\n` != escaped {
        t.Fatalf("expected the raw byte spelled beside the newline, got %q", escaped)
    }

    if true == strings.ContainsRune(escaped, utf8.RuneError) {
        t.Fatalf("the raw byte was replaced by U+FFFD: %q", escaped)
    }
}

/* a genuine U+FFFD is three valid bytes and ordinary text: the invalid-byte rule reads the decoder's width, so a replacement rune the server sent as itself passes through as itself, beside a raw byte or alone. */
func TestEscapeControlCharactersLeavesAGenuineReplacementRuneUntouched(t *testing.T) {
    if "a\xef\xbf\xbdb" != escapeControlCharacters("a\xef\xbf\xbdb", false) {
        t.Fatalf("expected a genuine U+FFFD to pass through unchanged, got %q", escapeControlCharacters("a\xef\xbf\xbdb", false))
    }

    if "a\xef\xbf\xbdb"+`\x9b` != escapeControlCharacters("a\xef\xbf\xbdb\x9b", false) {
        t.Fatalf("expected the genuine U+FFFD kept and the raw byte spelled, got %q", escapeControlCharacters("a\xef\xbf\xbdb\x9b", false))
    }
}

/* the early return answers a value it finds nothing to escape in with the value itself, so it has to see an invalid byte the way the loop does: a raw byte and nothing else is the shape that tells the two apart */
func TestContainsControlCharacterSeesARawByte(t *testing.T) {
    if false == containsControlCharacter("\x9b", false) {
        t.Fatal("a raw byte must be reported as a control character, or the early return hands it back as sent")
    }

    if `\x9b` != escapeControlCharacters("\x9b", false) {
        t.Fatalf("the early return handed back the raw byte: %q", escapeControlCharacters("\x9b", false))
    }
}

/* the multi-line form keeps the real line break the query rendering exists for and nothing else, a raw byte beside it included */
func TestEscapeControlCharactersKeepsTheNewlineBesideARawByte(t *testing.T) {
    escaped := escapeControlCharacters("a\x9bb\nc", true)

    if `a\x9bb`+"\nc" != escaped {
        t.Fatalf("expected the raw byte spelled and the line break kept, got %q", escaped)
    }
}
