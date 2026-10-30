package encrypt

import (
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/exception"
)

func TestStaticKeyProvider_ActiveKeyIdsCurrentFirst(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1), "v2": newKey(2), "v3": newKey(3)}})

    active := provider.ActiveKeyIds()
    if 3 != len(active) || "v2" != active[0] {
        t.Fatalf("expected current key first, got %v", active)
    }
}

/* The %#v and %v verbs walk unexported fields, so a provider dropped into a debug log — or into an error context that is later formatted — would print every master key as raw bytes. This is the same redaction class the encrypted column types close; the keys are worth strictly more than the ciphertext they protect. */
func TestStaticKeyProvider_RedactsKeysUnderEveryVerb(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{
        CurrentKeyId: "current",
        Keys: map[string][]byte{
            "current": []byte("0123456789abcdef0123456789abcdef"),
        },
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

/* fmt routes only %v %s %q %x %X and %#v through Stringer/GoStringer; the numeric verbs (%d %o %b %c %U) consult fmt.Formatter alone, so without it they reflection-walk the unexported keysById field and dump the raw master key bytes — %d as decimal codes (48 49 50 ...), %c as the verbatim key characters. The Format method must redact every verb on both the value and the pointer form. */
func TestStaticKeyProvider_RedactsKeysUnderNumericVerbs(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{
        CurrentKeyId: "current",
        Keys: map[string][]byte{
            "current": []byte("0123456789abcdef0123456789abcdef"),
        },
    })

    redacted := provider.String()

    cases := []struct {
        name     string
        rendered string
    }{
        {"decimal pointer", fmt.Sprintf("%d", provider)},
        {"decimal value", fmt.Sprintf("%d", *provider)},
        {"octal pointer", fmt.Sprintf("%o", provider)},
        {"octal value", fmt.Sprintf("%o", *provider)},
        {"binary pointer", fmt.Sprintf("%b", provider)},
        {"binary value", fmt.Sprintf("%b", *provider)},
        {"character pointer", fmt.Sprintf("%c", provider)},
        {"character value", fmt.Sprintf("%c", *provider)},
        {"unicode pointer", fmt.Sprintf("%U", provider)},
        {"unicode value", fmt.Sprintf("%U", *provider)},
    }

    for _, testCase := range cases {
        /* A numeric verb that reached the key would print the byte "48" (the decimal code of '0') for %d and the character "a" for %c; the redacted form contains neither. */
        if true == strings.Contains(testCase.rendered, "48 49 50 51") {
            t.Fatalf("%s: a master key leaked as decimal bytes through fmt: %s", testCase.name, testCase.rendered)
        }

        if redacted != testCase.rendered {
            t.Fatalf("%s: expected the redacted rendering %q, got %q", testCase.name, redacted, testCase.rendered)
        }
    }
}

func TestStaticKeyProvider_RefusesAnAllZeroKey(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the all-zero key to be refused")
        }

        recoveredErr, isErr := recovered.(error)
        if false == isErr || false == strings.Contains(recoveredErr.Error(), "encryption key is all zero bytes") || "zeroed" != exception.LogContext(recoveredErr, exception.LogContextConfig{})["keyId"] {
            t.Fatalf("expected the refusal to name the shape and the key id, got: %v", recovered)
        }
    }()

    NewStaticKeyProvider(KeyRing{CurrentKeyId: "zeroed", Keys: map[string][]byte{"zeroed": make([]byte, 32)}})
}

/* a key of one repeated non-zero byte is accepted on purpose: the door refuses the one shape a key that was never generated has, and judges nothing else about a key that is the operator's */
func TestStaticKeyProvider_AcceptsAKeyOfOneRepeatedByte(t *testing.T) {
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "filled", Keys: map[string][]byte{"filled": newKey(1)}})

    if "filled" != provider.CurrentKeyId() {
        t.Fatalf("expected the repeated-byte key to be accepted")
    }
}

func TestStaticKeyProvider_RefusesARingNamingTheField(t *testing.T) {
    testCases := []struct {
        name    string
        ring    KeyRing
        message string
        field   string
        keyId   any
    }{
        {name: "empty ring", ring: KeyRing{}, message: "current key id is empty", field: "KeyRing.CurrentKeyId", keyId: nil},
        {name: "current id without a key", ring: KeyRing{CurrentKeyId: "v2", Keys: map[string][]byte{"v1": newKey(1)}}, message: "current key id has no key", field: "KeyRing.CurrentKeyId", keyId: "v2"},
        {name: "id outside the grammar", ring: KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": newKey(1), "v:2": newKey(2)}}, message: "key id must match", field: "KeyRing.Keys", keyId: "v:2"},
        {name: "short key", ring: KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": []byte("short")}}, message: "encryption key must be 32 bytes", field: "KeyRing.Keys", keyId: "v1"},
        {name: "zero key", ring: KeyRing{CurrentKeyId: "v1", Keys: map[string][]byte{"v1": make([]byte, 32)}}, message: "encryption key is all zero bytes", field: "KeyRing.Keys", keyId: "v1"},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            defer func() {
                recovered := recover()
                recoveredErr, isErr := recovered.(error)
                if false == isErr || false == strings.Contains(recoveredErr.Error(), testCase.message) {
                    t.Fatalf("expected the refusal %q, got %v", testCase.message, recovered)
                }

                logContext := exception.LogContext(recoveredErr, exception.LogContextConfig{})
                if testCase.field != logContext["field"] || testCase.keyId != logContext["keyId"] {
                    t.Fatalf("expected field %q and key id %v, got %v", testCase.field, testCase.keyId, logContext)
                }
            }()

            NewStaticKeyProvider(testCase.ring)
        })
    }
}

func TestStaticKeyProvider_CopiesTheRingsKeys(t *testing.T) {
    keys := map[string][]byte{"v1": newKey(1)}
    provider := NewStaticKeyProvider(KeyRing{CurrentKeyId: "v1", Keys: keys})

    keys["v1"][0] = 0
    keys["v2"] = newKey(2)

    held, keyErr := provider.Key("v1")
    if nil != keyErr || 1 != held[0] {
        t.Fatalf("expected the held key unchanged: %v %v", held, keyErr)
    }

    if _, missingErr := provider.Key("v2"); nil == missingErr {
        t.Fatalf("expected a key added to the caller's map after the build to stay out")
    }
}
