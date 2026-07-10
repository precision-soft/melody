package config

import (
    "strings"
    "os"
    "path/filepath"
    "testing"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
)

func TestEnvironmentContractIsUsed(t *testing.T) {
    var _ configcontract.EnvironmentSource = (*testEnvironmentSource)(nil)
}

func TestPreprocessDotEnvContent_InlineHashWithoutLeadingSpaceIsKept(t *testing.T) {
    processed, err := preprocessDotEnvContent("COLOR=#ffffff\nPASSWORD=ab#cd")
    if nil != err {
        t.Fatalf("unexpected error: %s", err.Error())
    }

    expected := "COLOR=#ffffff\nPASSWORD=ab#cd"
    if expected != processed {
        t.Fatalf("expected %q, got %q", expected, processed)
    }
}

func TestPreprocessDotEnvContent_WhitespacePrecededHashIsComment(t *testing.T) {
    processed, err := preprocessDotEnvContent("KEY=value # trailing comment\n# full line comment\nOTHER=1")
    if nil != err {
        t.Fatalf("unexpected error: %s", err.Error())
    }

    expected := "KEY=value\nOTHER=1"
    if expected != processed {
        t.Fatalf("expected %q, got %q", expected, processed)
    }
}

func TestLoadExistingDotEnvFile_PreservesQuotedWhitespace(t *testing.T) {
    directory := t.TempDir()

    writeErr := os.WriteFile(filepath.Join(directory, ".env"), []byte("PADDED=\"  spaced  \"\n"), 0o600)
    if nil != writeErr {
        t.Fatalf("write env file: %s", writeErr.Error())
    }

    source := NewEnvironmentSource(os.DirFS(directory), "")
    values := make(map[string]string)

    if loadErr := source.loadExistingDotEnvFile(values, ".env"); nil != loadErr {
        t.Fatalf("load env file: %s", loadErr.Error())
    }

    if "  spaced  " != values["PADDED"] {
        t.Fatalf("expected quoted whitespace preserved, got %q", values["PADDED"])
    }
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
