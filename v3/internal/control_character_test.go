package internal

import (
    "bytes"
    "encoding/json"
    "fmt"
    "reflect"
    "strings"
    "testing"
    "unicode/utf8"
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

/* the C1 introducer reaches the escaping as a raw byte at least as readily as in its two-byte encoding — a header value admits 0x9b, and a percent-encoded path segment is decoded to it before it enters a log context — and a walk over runes read that byte as U+FFFD, outside every range the guard checks, so the promise the GoDoc makes for \x9b was kept for the encoded spelling alone. Every byte that starts no valid sequence is spelled \xNN, and what comes out is valid UTF-8. */
func TestEscapeControlCharacters_EscapesARawByteThatIsNotValidUtf8(t *testing.T) {
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
        escaped := EscapeControlCharacters(currentCase.value)

        if currentCase.expected != escaped {
            t.Fatalf("%s: expected %q, got %q", currentCase.name, currentCase.expected, escaped)
        }

        if false == utf8.ValidString(escaped) {
            t.Fatalf("%s: expected valid UTF-8 out of the escaping, got %q", currentCase.name, escaped)
        }
    }
}

/* once another control character forced the rewrite, the raw byte was written out as U+FFFD: the same input was kept as sent when the byte stood alone and corrupted when a newline stood beside it, and what the client sent was destroyed instead of shown. Neither form may carry the replacement rune. */
func TestEscapeControlCharacters_ARawByteIsNotReplacedWhenAnotherCharacterForcesTheRewrite(t *testing.T) {
    escaped := EscapeControlCharacters("a\x9bb\n")

    if `a\x9bb\n` != escaped {
        t.Fatalf("expected the raw byte spelled beside the newline, got %q", escaped)
    }

    if true == strings.ContainsRune(escaped, utf8.RuneError) {
        t.Fatalf("the raw byte was replaced by U+FFFD: %q", escaped)
    }
}

/* a genuine U+FFFD is three valid bytes and ordinary text: the invalid-byte rule reads the decoder's width, so the replacement rune a client sent as itself passes through as itself, beside a raw byte or alone. */
func TestEscapeControlCharacters_LeavesAGenuineReplacementRuneUntouched(t *testing.T) {
    if "a\xef\xbf\xbdb" != EscapeControlCharacters("a\xef\xbf\xbdb") {
        t.Fatalf("expected a genuine U+FFFD to pass through unchanged, got %q", EscapeControlCharacters("a\xef\xbf\xbdb"))
    }

    if "a\xef\xbf\xbdb"+`\x9b` != EscapeControlCharacters("a\xef\xbf\xbdb\x9b") {
        t.Fatalf("expected the genuine U+FFFD kept and the raw byte spelled, got %q", EscapeControlCharacters("a\xef\xbf\xbdb\x9b"))
    }
}

/* the early return answers a value it finds nothing to escape in with the value itself, so it has to see an invalid byte the way the loop does: a raw byte and nothing else is the shape that tells the two apart. */
func TestEscapeControlCharacters_TheEarlyReturnSeesARawByte(t *testing.T) {
    escaped := EscapeControlCharacters("\x9b")

    if `\x9b` != escaped {
        t.Fatalf("the early return handed back the raw byte: %q", escaped)
    }
}

/* the cell form keeps the real line break and nothing else, a raw byte beside it included */
func TestEscapeControlCharactersKeepingNewlines_EscapesARawByteAndKeepsTheLineBreak(t *testing.T) {
    escaped := EscapeControlCharactersKeepingNewlines("a\x9bb\nc")

    if `a\x9bb`+"\nc" != escaped {
        t.Fatalf("expected the raw byte spelled and the line break kept, got %q", escaped)
    }
}

/* encoding/json escapes the C0 block and the two Unicode line separators and emits the C1 block raw, so a document carrying U+009B repainted the terminal it was printed to. The rewrite spells the rune as the escape the encoder uses for its own set — in a value, in a key and at both ends of the block — and the decoded document is the one the encoder was given. */
func TestEscapeJsonC1Block_SpellsEveryC1RuneAsAJsonEscape(t *testing.T) {
    original := map[string]string{
        "k\xc2\x9dey": "a\xc2\x9bb",
        "nel":         "x\xc2\x85y",
        "pad":         "\xc2\x80",
        "apc":         "\xc2\x9f",
    }

    document, marshalErr := json.Marshal(original)
    if nil != marshalErr {
        t.Fatalf("unexpected marshal error: %v", marshalErr)
    }

    escaped := EscapeJsonC1Block(document)

    for _, continuation := range []byte{0x80, 0x85, 0x9b, 0x9d, 0x9f} {
        if true == bytes.Contains(escaped, []byte{0xc2, continuation}) {
            t.Fatalf("a raw C1 rune c2 %02x survived in %q", continuation, escaped)
        }

        spelling := "\\" + "u00" + fmt.Sprintf("%02x", continuation)
        if false == bytes.Contains(escaped, []byte(spelling)) {
            t.Fatalf("expected the spelling %s in %q", spelling, escaped)
        }
    }

    decoded := map[string]string{}
    if decodeErr := json.Unmarshal(escaped, &decoded); nil != decodeErr {
        t.Fatalf("expected the rewritten document to decode, got %v for %q", decodeErr, escaped)
    }

    if false == reflect.DeepEqual(original, decoded) {
        t.Fatalf("expected the decoded document to equal the encoded one, got %#v", decoded)
    }
}

/* the lead byte of the block also opens the no-break space and the Latin-1 punctuation, and a lead byte at the very end of the document has no continuation to read: none of them is a C1 rune, and a document without one is answered as it was given. */
func TestEscapeJsonC1Block_LeavesEveryOtherByteAsItWas(t *testing.T) {
    for _, document := range [][]byte{
        []byte("\"caf\xc3\xa9 \xc2\xa0 \xc2\xbf tab\\t line\\" + "u2028\""),
        []byte("\"x\xc2"),
        []byte(`{"plain":"ascii"}`),
    } {
        if false == bytes.Equal(document, EscapeJsonC1Block(document)) {
            t.Fatalf("expected %q to be answered unchanged, got %q", document, EscapeJsonC1Block(document))
        }
    }
}
