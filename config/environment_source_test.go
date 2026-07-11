package config

import (
    "strings"
    "testing"

    configcontract "github.com/precision-soft/melody/config/contract"
)

func TestEnvironmentContractIsUsed(t *testing.T) {
    var _ configcontract.EnvironmentSource = (*testEnvironmentSource)(nil)
}

/** @info godotenv understands a quoted value that spans lines. The comment stripper tracked quote state per physical line, so from the second line on it believed it was outside quotes: a '#' inside the value opened a comment, and a line that became blank was dropped, silently truncating the value. */
func TestPreprocessDotEnvContent_KeepsMultilineQuotedValues(t *testing.T) {
    content := "KEY=\"first\n# not a comment\n\nlast\"\nOTHER=plain # trailing comment\n"

    processed, err := preprocessDotEnvContent(content)
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    if false == strings.Contains(processed, "# not a comment") {
        t.Fatalf("a '#' inside a quoted value is data, not a comment: %q", processed)
    }
    if false == strings.Contains(processed, "last\"") {
        t.Fatalf("the quoted value lost its tail: %q", processed)
    }
    if true == strings.Contains(processed, "trailing comment") {
        t.Fatalf("a real trailing comment must still be stripped: %q", processed)
    }
}

/** @info An editor that saves .env as UTF-8 with a byte order mark makes godotenv reject the first line ("unexpected character in variable name"), so boot fails on a file that looks perfectly correct. U+FEFF is not whitespace, so no TrimSpace removes it. */
func TestPreprocessDotEnvContent_StripsTheByteOrderMark(t *testing.T) {
    processed, err := preprocessDotEnvContent("\ufeffMELODY_ENV=prod\nFOO=bar\n")
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    if true == strings.ContainsRune(processed, '\ufeff') {
        t.Fatalf("the byte order mark survived preprocessing: %q", processed)
    }
    if false == strings.HasPrefix(processed, "MELODY_ENV=prod") {
        t.Fatalf("expected the first assignment to be parseable, got %q", processed)
    }
}

/** @info godotenv skips a backslash-escaped quote inside a value, so it does not terminate the string. The stripper toggled its quote state on every quote, so an escaped quote flipped it to "outside quotes"; a following space-preceded '#' was then read as a comment and the valid value was truncated. */
func TestPreprocessDotEnvContent_KeepsBackslashEscapedQuotesInsideValues(t *testing.T) {
    content := `CONFIG="prefix \"section # note\" suffix"` + "\n"

    processed, err := preprocessDotEnvContent(content)
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    expected := `CONFIG="prefix \"section # note\" suffix"`
    if expected != processed {
        t.Fatalf("an escaped quote must not terminate the value, so the '#' inside it is data: expected %q, got %q", expected, processed)
    }
}

/** @info godotenv opens a quoted value only when the quote is the first character of the value; a stray quote in an unquoted value is literal. The stripper opened quote state on any quote, so an unbalanced quote in one value flipped the state for the rest of the file: a later genuinely-quoted multiline value then had a '#'-prefixed interior line silently dropped. */
func TestPreprocessDotEnvContent_LiteralQuoteInUnquotedValueDoesNotSpanLines(t *testing.T) {
    content := "NOTE=say \"hello\nB=\"line1\n# data line\nline2\"\n"

    processed, err := preprocessDotEnvContent(content)
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    if false == strings.Contains(processed, "# data line") {
        t.Fatalf("a stray quote in NOTE must not flip cross-line state and drop B's interior line: %q", processed)
    }
    if false == strings.Contains(processed, "NOTE=say \"hello") {
        t.Fatalf("the unquoted NOTE value with a literal quote must be preserved: %q", processed)
    }
}
