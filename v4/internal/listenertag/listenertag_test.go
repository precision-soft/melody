package listenertag

import (
    "testing"
)

func TestRefused(t *testing.T) {
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
        {"the framework's own tags", []string{FrameworkAccessLog, FrameworkCors}, "", 0, "", false},
        {"empty tag", []string{"audit", ""}, RefusalEmpty, 1, "", true},
        {"whitespace", []string{"a b"}, RefusalWhitespace, 0, "a b", true},
        {"control character", []string{"audit", "a\tb"}, RefusalWhitespace, 1, "a\tb", true},
        {"reserved", []string{"audit", "melody.audit"}, RefusalReserved, 1, "melody.audit", true},
        {"the bare prefix is reserved", []string{FrameworkPrefix}, RefusalReserved, 0, FrameworkPrefix, true},
        {"whitespace is read before the namespace", []string{"melody. x"}, RefusalWhitespace, 0, "melody. x", true},
        {"the namespace is read before a repetition", []string{"melody.x", "melody.x"}, RefusalReserved, 0, "melody.x", true},
        {"a framework tag listed twice", []string{FrameworkCors, FrameworkCors}, RefusalTwice, 1, FrameworkCors, true},
        {"twice", []string{"audit", "cache", "audit"}, RefusalTwice, 2, "audit", true},
    } {
        reason, index, tag, refused := Refused(testCase.tags)
        if testCase.refused != refused || testCase.reason != reason || testCase.index != index || testCase.tag != tag {
            t.Fatalf("%s: expected %q %d %q %v, got %q %d %q %v", testCase.name, testCase.reason, testCase.index, testCase.tag, testCase.refused, reason, index, tag, refused)
        }
    }
}

func TestFrameworkTags_AnswersACopyOfTheCallersOwn(t *testing.T) {
    tags := FrameworkTags()
    tags[0] = "changed"

    if FrameworkAccessLog != FrameworkTags()[0] || 2 != len(FrameworkTags()) {
        t.Fatalf("expected the two framework tags, got %v", FrameworkTags())
    }
}
