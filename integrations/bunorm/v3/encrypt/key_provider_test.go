package encrypt

import (
    "fmt"
    "strings"
    "testing"
)

func TestStaticKeyProvider_ActiveKeyIdsCurrentFirst(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2), "v3": newKey(3)})

    active := provider.ActiveKeyIds()
    if 3 != len(active) || "v2" != active[0] {
        t.Fatalf("expected current key first, got %v", active)
    }
}

/** @info The %#v and %v verbs walk unexported fields, so a provider dropped into a debug log — or into an error context that is later formatted — printed every master key as raw bytes. This is the same redaction class the encrypted column types close; the keys are worth strictly more than the ciphertext they protect. */
func TestStaticKeyProvider_RedactsKeysUnderEveryVerb(t *testing.T) {
    provider := NewStaticKeyProvider("current", map[string][]byte{
        "current": []byte("0123456789abcdef0123456789abcdef"),
    })

    for _, rendered := range []string{
        fmt.Sprintf("%#v", provider),
        fmt.Sprintf("%v", provider),
        fmt.Sprintf("%s", provider),
        fmt.Sprintf("%#v", *provider),
        fmt.Sprintf("%v", *provider),
    } {
        if true == strings.Contains(rendered, "0123456789abcdef") {
            t.Fatalf("a master key leaked through fmt: %s", rendered)
        }
    }
}
