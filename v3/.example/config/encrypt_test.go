package config

import (
    "fmt"
    "strings"
    "testing"

    melodyexception "github.com/precision-soft/melody/v3/exception"
)

func TestEncryptKeys_ReadsEveryListedKeyAndTheCurrentOne(t *testing.T) {
    currentKeyId, keysById := encryptKeys("example-2026:melody-example-cipher-key-32byte, example-2027:a:key-with-a-colon-in-it-32bytes!", "example-2027")

    if "example-2027" != currentKeyId || 2 != len(keysById) {
        t.Fatalf("expected two keys and example-2027 current, got %q over %d keys", currentKeyId, len(keysById))
    }

    if "melody-example-cipher-key-32byte" != string(keysById["example-2026"]) || "a:key-with-a-colon-in-it-32bytes!" != string(keysById["example-2027"]) {
        t.Fatalf("expected each key read after the first colon of its entry, got %q", keysById)
    }
}

func TestEncryptKeys_FallsBackToTheDevelopmentKeyWithoutAList(t *testing.T) {
    currentKeyId, keysById := encryptKeys("  ", "ignored")

    if encryptDevelopmentKeyId != currentKeyId || 1 != len(keysById) || encryptDevelopmentKey != string(keysById[encryptDevelopmentKeyId]) {
        t.Fatalf("expected the development key alone, got %q over %q", currentKeyId, keysById)
    }
}

func TestEncryptKeys_RefusesAMalformedListNamingTheEntryAndNeverTheKey(t *testing.T) {
    for _, rawKeyList := range []string{"example-2026", "example-2026:", ":melody-example-cipher-key-32byte", "a:melody-example-cipher-key-32byte,a:melody-example-cipher-key-32byte"} {
        func() {
            defer func() {
                recovered := recover()
                if nil == recovered {
                    t.Fatalf("%q: expected the list refused", rawKeyList)
                }

                refusal, isError := recovered.(error)
                if false == isError {
                    t.Fatalf("%q: expected an error, got %v", rawKeyList, recovered)
                }

                logged := fmt.Sprintf("%v", melodyexception.LogContext(refusal))
                if true == strings.Contains(refusal.Error(), "cipher-key") || true == strings.Contains(logged, "cipher-key") {
                    t.Fatalf("%q: the refusal names the key: %v %v", rawKeyList, refusal, logged)
                }
            }()

            encryptKeys(rawKeyList, "example-2026")
        }()
    }
}
