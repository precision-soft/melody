package accesscontrol

import (
    "testing"
)

/* the guard asks the PARSED expression, not the spelling, because the two disagree exactly where it matters: "^/public|/status" begins with "^" and is read by Go as (^/public)|(/status), whose second branch floats anywhere. Each unanchored case below is one a public rule must not be declared on; each anchored case is one the previous textual test refused by mistake. */
func TestPatternIsAnchoredToPathStart(t *testing.T) {
    for _, testCase := range []struct {
        pattern  string
        anchored bool
        why      string
    }{
        {"^/public", true, "the plain anchor"},
        {"^/public(/|$)", true, "the segment-boundary idiom the godoc recommends"},
        {"(?i)^/public(/|$)", true, "a flag group in front of the anchor does not move the start"},
        {`\A/public`, true, `\A anchors the text like ^`},
        {"^/public|^/status", true, "every branch of the alternation carries its own anchor"},
        {"^(/public|/status)", true, "the alternation sits inside the anchored concatenation"},
        {"^/a+", true, "a repetition starts where its element starts"},

        {"/status", false, "no anchor at all"},
        {"^/public|/status", false, "the second branch floats: it is the whole hole"},
        {"/status|^/public", false, "the floating branch is first"},
        {"^/a|/b|^/c", false, "one floating branch among three"},
        {"(?m)^/public", false, "in multiline mode ^ also matches after a newline, so it is not the path start"},
    } {
        if testCase.anchored != patternIsAnchoredToPathStart(testCase.pattern) {
            t.Fatalf(
                "expected %q to read as anchored=%v (%s)",
                testCase.pattern,
                testCase.anchored,
                testCase.why,
            )
        }
    }
}

/* the refusal is what keeps a public rule from reaching into a protected path: among regex rules the first registered that matches wins, so a public substring rule shadows every stricter regex declared after it. */
func TestNewRegexRule_RefusesPublicAccessOnAPatternThatCanMatchInsideAPath(t *testing.T) {
    for _, pattern := range []string{"/status", "^/public|/status", "(?m)^/public"} {
        func() {
            defer func() {
                if nil == recover() {
                    t.Fatalf("expected the pattern %q to be refused for PUBLIC_ACCESS", pattern)
                }
            }()

            _ = NewRegexRule(pattern, RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
        }()
    }
}

func TestNewRegexRule_AcceptsPublicAccessOnAnAnchoredPattern(t *testing.T) {
    for _, pattern := range []string{"^/public", "^/public(/|$)", "(?i)^/public(/|$)", `\A/public`} {
        func() {
            defer func() {
                if recovered := recover(); nil != recovered {
                    t.Fatalf("expected the anchored pattern %q to be accepted, got %v", pattern, recovered)
                }
            }()

            _ = NewRegexRule(pattern, RuleConfig{Attributes: []string{"PUBLIC_ACCESS"}})
        }()
    }
}

/* a non-public rule is not the matcher's concern: the anchor question exists to stop a rule from GRANTING inside a protected path, and an unanchored role rule only ever demands more. */
func TestNewRegexRule_AcceptsAnUnanchoredPatternForARoleAttribute(t *testing.T) {
    defer func() {
        if recovered := recover(); nil != recovered {
            t.Fatalf("expected an unanchored role rule to be accepted, got %v", recovered)
        }
    }()

    _ = NewRegexRule("/status", RuleConfig{Attributes: []string{"ROLE_USER"}})
}
