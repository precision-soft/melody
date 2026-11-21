package httpclient

import (
    "strings"
    "testing"
)

func TestCanonicalizeHeaderMap_CanonicalizesTheKeys(t *testing.T) {
    canonical, err := canonicalizeHeaderMap(map[string]string{"x-api-key": "secret"})

    if nil != err || "secret" != canonical["X-Api-Key"] {
        t.Fatalf("expected the key canonicalized, got %#v (%v)", canonical, err)
    }
}

func TestCanonicalizeHeaderMap_AnswersTheCollisionAsAnErrorAndNoMap(t *testing.T) {
    canonical, err := canonicalizeHeaderMap(map[string]string{
        "x-api-key": "old",
        "X-Api-Key": "new",
    })

    if nil == err {
        t.Fatal("expected the collision to be refused")
    }

    if false == strings.Contains(err.Error(), "collide") {
        t.Fatalf("expected the refusal to name the collision, got %v", err)
    }

    if nil != canonical {
        t.Fatalf("expected no map beside the refusal, got %#v", canonical)
    }
}
