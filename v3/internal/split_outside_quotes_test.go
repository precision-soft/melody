package internal

import (
    "strings"
    "testing"
)

/* a separator inside a quoted parameter value belongs to that value: without quote awareness the media range text/plain;version="1,2";q=0 split into two members and the refusal it carried travelled with the junk half */
func TestSplitOutsideQuotes_HonoursQuotedSections(t *testing.T) {
    parts := SplitOutsideQuotes(`text/plain;version="1,2";q=0, application/json`, ',')
    if 2 != len(parts) {
        t.Fatalf("expected 2 members, got %d: %q", len(parts), parts)
    }

    parameters := SplitOutsideQuotes(`version="a;b";q=1`, ';')
    if 2 != len(parameters) {
        t.Fatalf("expected 2 parameters, got %d: %q", len(parameters), parameters)
    }

    escaped := SplitOutsideQuotes(`p="a\",b";q=1`, ',')
    if 1 != len(escaped) {
        t.Fatalf("expected the escaped quote to keep the member whole, got %d: %q", len(escaped), escaped)
    }
}

func TestSplitOutsideQuotes_CapsTheMemberCount(t *testing.T) {
    /* an unauthenticated header of nothing but separators must not become a member per byte: the
       negotiating readers cut a short list, so the split is bounded far above any real header and the
       remainder past the cap stays one final member */
    hostileValue := strings.Repeat(",", 100000)

    members := SplitOutsideQuotes(hostileValue, ',')

    if maximumSplitOutsideQuotesMembers < len(members) {
        t.Fatalf("expected at most %d members, got %d", maximumSplitOutsideQuotesMembers, len(members))
    }
}

func TestSplitOutsideQuotes_ALegitimateListIsNotTruncated(t *testing.T) {
    /* a real header of a few members must split whole — the cap only bites pathological input */
    members := SplitOutsideQuotes("text/html, application/json, text/plain;q=0.5, */*;q=0.1", ',')

    if 4 != len(members) {
        t.Fatalf("expected 4 members, got %d: %v", len(members), members)
    }
}
