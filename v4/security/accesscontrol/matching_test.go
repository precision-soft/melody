package accesscontrol

import (
    "testing"
)

func TestMatching_StringNamesEveryMode(t *testing.T) {
    for matching, name := range map[Matching]string{
        MatchingUnspecified:   "unspecified",
        MatchingExact:         "exact",
        MatchingSegmentPrefix: "segment prefix",
        MatchingRawPrefix:     "raw prefix",
        MatchingRegex:         "regex",
        Matching(99):          "unspecified",
    } {
        if name != matching.String() {
            t.Fatalf("expected %q, got %q", name, matching.String())
        }
    }
}
