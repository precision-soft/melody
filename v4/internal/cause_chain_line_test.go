package internal

import (
    "strings"
    "testing"
)

func TestAppendCauseChain_AppendsEveryLinkInOrder(t *testing.T) {
    line := AppendCauseChain("request failed", []string{"store unavailable", "dial tcp: refused"}, 8)

    if "request failed: cause 1: store unavailable: cause 2: dial tcp: refused" != line {
        t.Fatalf("unexpected line %q", line)
    }
}

func TestAppendCauseChain_LeavesOutALinkTheTextBeforeItEndsWith(t *testing.T) {
    line := AppendCauseChain("saving the user: store unavailable: dial tcp: refused", []string{"store unavailable: dial tcp: refused", "dial tcp: refused", "connection reset"}, 8)

    if "saving the user: store unavailable: dial tcp: refused: cause 1: connection reset" != line {
        t.Fatalf("unexpected line %q", line)
    }
}

func TestAppendCauseChain_KeepsAShortCauseTheMessageOnlyContains(t *testing.T) {
    cases := map[string][]string{
        "unexpected EOF in header": {"EOF"},
        "failed to load 10 rows":   {"1"},
        "[error] boom":             {"error"},
        "boom":                     {"b"},
        "failed":                   {"cause 1"},
    }

    for message, chain := range cases {
        if line := AppendCauseChain(message, chain, 8); message+": cause 1: "+chain[0] != line {
            t.Fatalf("expected the short cause kept for %q, got %q", message, line)
        }
    }
}

func TestAppendCauseChain_EscapesALinkToOneLineAndThenBoundsIt(t *testing.T) {
    line := AppendCauseChain("failed", []string{"first\nsecond", strings.Repeat("\x01", 600)}, 8)

    if true == strings.Contains(line, "\n") {
        t.Fatalf("expected one line, got %q", line)
    }

    if false == strings.Contains(line, "cause 1: first\\nsecond") {
        t.Fatalf("expected the escaped link, got %q", line)
    }

    second := line[strings.Index(line, "cause 2: ")+len("cause 2: "):]
    if false == strings.Contains(second, "...(truncated ") || 600 < len(second) {
        t.Fatalf("expected the escaped link cut near 512 bytes, got %d bytes", len(second))
    }
}

func TestAppendCauseChain_StopsAtTheMaximumOfPrintedLinks(t *testing.T) {
    line := AppendCauseChain("boom", []string{"boom", "first cause", "second cause"}, 1)

    if "boom: cause 1: first cause" != line {
        t.Fatalf("expected one printed link, the repeated one not counted, got %q", line)
    }
}

func TestAppendCauseChain_AnEmptyChainLeavesTheLine(t *testing.T) {
    if "failed" != AppendCauseChain("failed", nil, 8) {
        t.Fatal("expected the line unchanged")
    }
}
