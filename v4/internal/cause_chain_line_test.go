package internal

import (
    "strings"
    "testing"
)

func TestAppendCauseChain_AppendsEveryLinkInOrder(t *testing.T) {
    line := AppendCauseChain("request failed", []string{"store unavailable", "dial tcp: refused"})

    if "request failed: cause 1: store unavailable: cause 2: dial tcp: refused" != line {
        t.Fatalf("unexpected line %q", line)
    }
}

func TestAppendCauseChain_LeavesOutALinkTheLineAlreadyCarries(t *testing.T) {
    line := AppendCauseChain("saving the user: store unavailable", []string{"store unavailable", "dial tcp: refused"})

    if "saving the user: store unavailable: cause 1: dial tcp: refused" != line {
        t.Fatalf("unexpected line %q", line)
    }
}

func TestAppendCauseChain_EscapesALinkToOneLineAndBoundsIt(t *testing.T) {
    line := AppendCauseChain("failed", []string{"first\nsecond", strings.Repeat("x", 600)})

    if true == strings.Contains(line, "\n") {
        t.Fatalf("expected one line, got %q", line)
    }

    if false == strings.Contains(line, "cause 1: first\\nsecond") || false == strings.Contains(line, "...(truncated 88 bytes)") {
        t.Fatalf("expected the escaped and bounded links, got %q", line)
    }
}

func TestAppendCauseChain_AnEmptyChainLeavesTheLine(t *testing.T) {
    if "failed" != AppendCauseChain("failed", nil) {
        t.Fatal("expected the line unchanged")
    }
}
