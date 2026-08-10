package serializer

import (
    "testing"
)

func TestNormalizeMime_StripsParametersAndLowercases(t *testing.T) {
    if "application/json" != normalizeMime("Application/Json; charset=utf-8") {
        t.Fatalf("unexpected mime")
    }
}

func TestParseAcceptHeader_SortsByQualityDescending(t *testing.T) {
    values := parseAcceptHeader("text/plain;q=0.2, application/json;q=0.9")

    if 2 != len(values) {
        t.Fatalf("unexpected length")
    }

    if "application/json" != values[0].mime {
        t.Fatalf("expected json first")
    }
    if "text/plain" != values[1].mime {
        t.Fatalf("expected text second")
    }
}

func TestParseAcceptHeader_DropsAMemberWithAMalformedQuality(t *testing.T) {
    values := parseAcceptHeader("application/json;q=abc, text/plain;q=0.5")

    if 1 != len(values) {
        t.Fatalf("expected the malformed member to be dropped, got %d members", len(values))
    }

    if "text/plain" != values[0].mime {
        t.Fatalf("expected the valid member to survive, got %s", values[0].mime)
    }
}

func TestWildcardSubtypeMatching(t *testing.T) {
    if false == isWildcardSubtype("application/*") {
        t.Fatalf("expected wildcard subtype")
    }

    if false == matchWildcardSubtype("application/*", "application/json") {
        t.Fatalf("expected match")
    }

    if true == matchWildcardSubtype("application/*", "text/plain") {
        t.Fatalf("expected no match")
    }
}

/* the members a header can carry that are not media ranges at all — an empty one from a doubled comma, one whose parameters are doubled semicolons, a parameter with no equals sign, and one that normalizes away to nothing because it was only ever a parameter list — each has its own skip in the loop. Together they are the shapes a hand-assembled or proxy-rewritten Accept header actually arrives in, and a skip that fell through instead would put a member with an empty mime into the negotiation, where the empty key matches nothing and the header silently loses the range that followed it. */
func TestParseAcceptHeader_SkipsTheMembersThatAreNotMediaRanges(t *testing.T) {
    parsed := parseAcceptHeader("application/json,,text/plain;;charset=utf-8;novalue,  ,;q=0.5")

    if 2 != len(parsed) {
        t.Fatalf("expected only the two real ranges to survive, got %d: %#v", len(parsed), parsed)
    }

    surviving := map[string]bool{}
    for _, member := range parsed {
        surviving[member.mime] = true
    }

    if false == surviving[MimeApplicationJson] || false == surviving[MimeTextPlain] {
        t.Fatalf("expected both real ranges to survive, got %#v", parsed)
    }

    if true == surviving[""] {
        t.Fatalf("expected no empty mime to enter the negotiation, got %#v", parsed)
    }
}

