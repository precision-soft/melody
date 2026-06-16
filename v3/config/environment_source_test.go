package config

import (
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
