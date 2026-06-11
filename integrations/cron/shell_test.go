package cron

import (
    "strings"
    "testing"
)

func TestShellQuoteIfNeededEmptyStringYieldsTwoQuotes(t *testing.T) {
    if "''" != shellQuoteIfNeeded("") {
        t.Fatalf("shellQuoteIfNeeded(\"\") = %q, want %q", shellQuoteIfNeeded(""), "''")
    }
}

func TestShellQuoteIfNeededLeavesSafeTokensUnchanged(t *testing.T) {
    safe := "command-name"

    if safe != shellQuoteIfNeeded(safe) {
        t.Fatalf("shellQuoteIfNeeded(%q) = %q, want unchanged", safe, shellQuoteIfNeeded(safe))
    }
}

func TestShellQuoteIfNeededQuotesWhenSpacePresent(t *testing.T) {
    token := "hello world"
    expected := "'hello world'"

    if expected != shellQuoteIfNeeded(token) {
        t.Fatalf("shellQuoteIfNeeded(%q) = %q, want %q", token, shellQuoteIfNeeded(token), expected)
    }
}

func TestShellQuoteIfNeededQuotesWhenMetacharPresent(t *testing.T) {
    token := "echo$HOME"
    quoted := shellQuoteIfNeeded(token)

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

    if expected != joinShellTokens([]string{"alpha", "with space", "beta"}) {
        t.Fatalf("joinShellTokens result = %q, want %q", joinShellTokens([]string{"alpha", "with space", "beta"}), expected)
    }
}
