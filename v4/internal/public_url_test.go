package internal

import (
    "testing"
)

func TestNormalizePublicUrl_AnswersABaseThatNormalizesToItself(t *testing.T) {
    for _, value := range []string{"http://[fe80::1%25eth0]/app/", "https://x/a%2Fb", "https://x/my%20app//", "https://x/über"} {
        normalized, refusal := NormalizePublicUrl(value)
        if "" != refusal {
            t.Fatalf("expected %q admitted, got %q", value, refusal)
        }

        again, againRefusal := NormalizePublicUrl(normalized)
        if "" != againRefusal || normalized != again {
            t.Fatalf("expected %q to normalize to itself, got %q, %q", normalized, again, againRefusal)
        }
    }
}

func TestNormalizePublicUrl_RefusesABaseWithNoHostName(t *testing.T) {
    for _, value := range []string{"https://:8443", "http://:80/app"} {
        if _, refusal := NormalizePublicUrl(value); "a public url names a host" != refusal {
            t.Fatalf("expected %q refused for its host, got %q", value, refusal)
        }
    }
}

func TestNormalizePublicUrl_RefusesABasePathWithAnEmptyOrDotSegment(t *testing.T) {
    for _, value := range []string{"https://x//app", "https://x/a/../b", "https://x/./app", "https://x/%2E%2E/app", "https://x/app//b/"} {
        if _, refusal := NormalizePublicUrl(value); "a public url's path carries no empty, \".\" or \"..\" segment" != refusal {
            t.Fatalf("expected %q refused for its path, got %q", value, refusal)
        }
    }
}
