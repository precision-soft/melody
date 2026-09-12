package httpclient

import (
    "strings"
    "testing"
)

func TestCanonicalHeaderMap_CanonicalizesTheKeys(t *testing.T) {
    canonical := canonicalHeaderMap(map[string]string{"x-api-key": "secret"})

    if "secret" != canonical["X-Api-Key"] {
        t.Fatalf("expected the key canonicalized, got %#v", canonical)
    }
}

func TestCanonicalHeaderMap_RefusesTwoSpellingsOfOneHeader(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatal("expected a panic for two spellings that collapse onto one header")
        }

        recoveredErr, isError := recovered.(error)
        if false == isError {
            t.Fatalf("expected the panic value to be an error, got %T", recovered)
        }

        message := recoveredErr.Error()
        if false == strings.Contains(message, "collide") {
            t.Fatalf("expected the refusal to name the collision, got %v", message)
        }
    }()

    _ = canonicalHeaderMap(map[string]string{
        "x-api-key": "old",
        "X-Api-Key": "new",
    })
}

func TestRequestOptions_SetHeaderStoresTheCanonicalKeyDeterministically(t *testing.T) {
    options := NewRequestOptions()
    options.SetHeader("x-api-key", "first")
    options.SetHeader("X-Api-Key", "second")

    headers := options.Headers()
    if 1 != len(headers) || "second" != headers["X-Api-Key"] {
        t.Fatalf("expected the sequential last write on one canonical key, got %#v", headers)
    }
}

/* The client's own setter is the other door into a header map that is applied with Set: a rotation spelled differently from the configured key used to leave both entries live, and which credential travelled was decided by map iteration order. */
func TestHttpClient_SetHeaderRotatesTheCredentialUnderItsCanonicalSpelling(t *testing.T) {
    client := NewHttpClient(NewHttpClientConfig("", 0, map[string]string{"X-Api-Key": "rotated-out"}))

    client.SetHeader("x-api-key", "rotated-in")

    if 1 != len(client.headers) || "rotated-in" != client.headers["X-Api-Key"] {
        t.Fatalf("expected one canonical entry holding the rotated-in credential, got %#v", client.headers)
    }
}
