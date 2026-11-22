package internal

import (
    "testing"
)

func TestRefusedListenerTag(t *testing.T) {
    for _, testCase := range []struct {
        name    string
        tags    []string
        reason  string
        index   int
        tag     string
        refused bool
    }{
        {"nil", nil, "", 0, "", false},
        {"empty list", []string{}, "", 0, "", false},
        {"valid", []string{"audit", "cache"}, "", 0, "", false},
        {"empty tag", []string{"audit", ""}, ListenerTagRefusalEmpty, 1, "", true},
        {"whitespace", []string{"a b"}, ListenerTagRefusalWhitespace, 0, "a b", true},
        {"control character", []string{"audit", "a\tb"}, ListenerTagRefusalWhitespace, 1, "a\tb", true},
        {"twice", []string{"audit", "cache", "audit"}, ListenerTagRefusalTwice, 2, "audit", true},
    } {
        reason, index, tag, refused := RefusedListenerTag(testCase.tags)
        if testCase.refused != refused || testCase.reason != reason || testCase.index != index || testCase.tag != tag {
            t.Fatalf("%s: expected %q %d %q %v, got %q %d %q %v", testCase.name, testCase.reason, testCase.index, testCase.tag, testCase.refused, reason, index, tag, refused)
        }
    }
}
