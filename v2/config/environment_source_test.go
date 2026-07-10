package config

import (
    "strings"
    "testing"

    configcontract "github.com/precision-soft/melody/v2/config/contract"
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
