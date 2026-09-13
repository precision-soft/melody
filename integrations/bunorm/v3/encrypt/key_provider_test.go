package encrypt

import (
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/exception"
)

func TestStaticKeyProvider_ActiveKeyIdsCurrentFirst(t *testing.T) {
    provider := NewStaticKeyProvider("v2", map[string][]byte{"v1": newKey(1), "v2": newKey(2), "v3": newKey(3)})

    active := provider.ActiveKeyIds()
    if 3 != len(active) || "v2" != active[0] {
        t.Fatalf("expected current key first, got %v", active)
    }
}

/* The %#v and %v verbs walk unexported fields, so a provider dropped into a debug log — or into an error context that is later formatted — printed every master key as raw bytes. This is the same redaction class the encrypted column types close; the keys are worth strictly more than the ciphertext they protect. */
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

/* fmt routes only %v %s %q %x %X and %#v through Stringer/GoStringer; the numeric verbs (%d %o %b %c %U) consult fmt.Formatter alone, so without it they reflection-walk the unexported keysById field and dump the raw master key bytes — %d as decimal codes (48 49 50 ...), %c as the verbatim key characters. The Format method must redact every verb on both the value and the pointer form. */
func TestStaticKeyProvider_RedactsKeysUnderNumericVerbs(t *testing.T) {
    provider := NewStaticKeyProvider("current", map[string][]byte{
        "current": []byte("0123456789abcdef0123456789abcdef"),
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
        /* The unpatched code prints the byte "48" (the decimal code of '0') for %d and the character "a" for %c; the redacted form contains neither. */
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
        if false == isErr || false == strings.Contains(recoveredErr.Error(), "encryption key is all zero bytes") || "zeroed" != exception.LogContext(recoveredErr)["keyId"] {
            t.Fatalf("expected the refusal to name the shape and the key id, got: %v", recovered)
        }
    }()

    NewStaticKeyProvider("zeroed", map[string][]byte{"zeroed": make([]byte, 32)})
}

/* a key of one repeated non-zero byte is accepted on purpose: the door refuses the one shape a key that was never generated has, and judges nothing else about a key that is the operator's */
func TestStaticKeyProvider_AcceptsAKeyOfOneRepeatedByte(t *testing.T) {
    provider := NewStaticKeyProvider("filled", map[string][]byte{"filled": newKey(1)})

    if "filled" != provider.CurrentKeyId() {
        t.Fatalf("expected the repeated-byte key to be accepted")
    }
}
