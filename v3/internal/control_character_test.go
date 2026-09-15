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

    for _, neighbour := range []string{"a~b", "a\u00a0b", "a\u00e9b"} {
        if neighbour != EscapeControlCharacters(neighbour) {
            t.Fatalf("expected a rune outside the block to pass through unchanged, got %q", EscapeControlCharacters(neighbour))
        }
    }
}

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

func TestEscapeControlCharacters_SpellsARuneAboveOneByteInFourDigits(t *testing.T) {
    escaped := EscapeControlCharacters("a\u2028b")

    if true == strings.Contains(escaped, `\x20`) {
        t.Fatalf("the separator was spelled as a space followed by two digits: %q", escaped)
    }

    if `a\u2028b` != escaped {
        t.Fatalf("expected the four-digit spelling, got %q", escaped)
    }
}

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

func TestEscapeControlCharactersKeepingNewlines_KeepsOnlyTheNewline(t *testing.T) {
    escaped := EscapeControlCharactersKeepingNewlines("one\u0085two\u2028three\nfour")

    if `one\x85two\u2028three`+"\nfour" != escaped {
        t.Fatalf("unexpected escaping: %q", escaped)
    }

    if false == strings.Contains(escaped, "\n") {
        t.Fatal("the cell form must keep the real line break it exists for")
    }
}

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

func TestEscapeControlCharacters_ARawByteIsNotReplacedWhenAnotherCharacterForcesTheRewrite(t *testing.T) {
    escaped := EscapeControlCharacters("a\x9bb\n")

    if `a\x9bb\n` != escaped {
        t.Fatalf("expected the raw byte spelled beside the newline, got %q", escaped)
    }

    if true == strings.ContainsRune(escaped, utf8.RuneError) {
        t.Fatalf("the raw byte was replaced by U+FFFD: %q", escaped)
    }
}

func TestEscapeControlCharacters_LeavesAGenuineReplacementRuneUntouched(t *testing.T) {
    if "a\xef\xbf\xbdb" != EscapeControlCharacters("a\xef\xbf\xbdb") {
        t.Fatalf("expected a genuine U+FFFD to pass through unchanged, got %q", EscapeControlCharacters("a\xef\xbf\xbdb"))
    }

    if "a\xef\xbf\xbdb"+`\x9b` != EscapeControlCharacters("a\xef\xbf\xbdb\x9b") {
        t.Fatalf("expected the genuine U+FFFD kept and the raw byte spelled, got %q", EscapeControlCharacters("a\xef\xbf\xbdb\x9b"))
    }
}

func TestEscapeControlCharacters_TheEarlyReturnSeesARawByte(t *testing.T) {
    escaped := EscapeControlCharacters("\x9b")

    if `\x9b` != escaped {
        t.Fatalf("the early return handed back the raw byte: %q", escaped)
    }
}

func TestEscapeControlCharactersKeepingNewlines_EscapesARawByteAndKeepsTheLineBreak(t *testing.T) {
    escaped := EscapeControlCharactersKeepingNewlines("a\x9bb\nc")

    if `a\x9bb`+"\nc" != escaped {
        t.Fatalf("expected the raw byte spelled and the line break kept, got %q", escaped)
    }
}

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
