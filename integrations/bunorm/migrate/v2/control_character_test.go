package migrate

import (
    "strings"
    "testing"
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
