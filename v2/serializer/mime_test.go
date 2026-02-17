package serializer

import "testing"

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
