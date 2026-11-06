package cron

import (
    "strings"
    "testing"
)

func TestShellQuoteIfNeededEmptyStringYieldsTwoQuotes(t *testing.T) {
    if "''" != ShellQuoteIfNeeded("") {
        t.Fatalf("ShellQuoteIfNeeded(\"\") = %q, want %q", ShellQuoteIfNeeded(""), "''")
    }
}

func TestShellQuoteIfNeededLeavesSafeTokensUnchanged(t *testing.T) {
    safe := "command-name"

    if safe != ShellQuoteIfNeeded(safe) {
        t.Fatalf("ShellQuoteIfNeeded(%q) = %q, want unchanged", safe, ShellQuoteIfNeeded(safe))
    }
}

func TestShellQuoteIfNeededQuotesWhenSpacePresent(t *testing.T) {
    token := "hello world"
    expected := "'hello world'"

    if expected != ShellQuoteIfNeeded(token) {
        t.Fatalf("ShellQuoteIfNeeded(%q) = %q, want %q", token, ShellQuoteIfNeeded(token), expected)
    }
}

func TestShellQuoteIfNeededQuotesWhenMetacharPresent(t *testing.T) {
    token := "echo$HOME"
    quoted := ShellQuoteIfNeeded(token)

    if false == strings.HasPrefix(quoted, "'") || false == strings.HasSuffix(quoted, "'") {
        t.Fatalf("expected single-quoted output for %q, got %q", token, quoted)
    }
}

func TestSingleQuoteEscapesEmbeddedSingleQuote(t *testing.T) {
    expected := `'it'\''s'`

    if expected != singleQuote("it's") {
        t.Fatalf("singleQuote(%q) = %q, want %q", "it's", singleQuote("it's"), expected)
    }
}

func TestJoinShellTokensJoinsWithSpaces(t *testing.T) {
    expected := "alpha 'with space' beta"

    if expected != JoinShellTokens([]string{"alpha", "with space", "beta"}) {
        t.Fatalf("JoinShellTokens result = %q, want %q", JoinShellTokens([]string{"alpha", "with space", "beta"}), expected)
    }
}
