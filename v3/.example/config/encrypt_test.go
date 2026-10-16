package config

import (
    "fmt"
    "strings"
    "testing"

    melodyexception "github.com/precision-soft/melody/v3/exception"
)

func TestEncryptKeys_ReadsEveryListedKeyAndTheCurrentOne(t *testing.T) {
    currentKeyId, keysById := encryptKeys("example-2026:melody-example-cipher-key-32byte, example-2027:a:key-with-a-colon-in-it-32bytes!", "example-2027", true)

    if "example-2027" != currentKeyId || 2 != len(keysById) {
        t.Fatalf("expected two keys and example-2027 current, got %q over %d keys", currentKeyId, len(keysById))
    }

    if "melody-example-cipher-key-32byte" != string(keysById["example-2026"]) || "a:key-with-a-colon-in-it-32bytes!" != string(keysById["example-2027"]) {
        t.Fatalf("expected each key read after the first colon of its entry, got %q", keysById)
    }
}

func TestEncryptKeys_FallsBackToTheDevelopmentKeyWithoutAListInDevelopmentOnly(t *testing.T) {
    currentKeyId, keysById := encryptKeys("  ", "ignored", true)

    if encryptDevelopmentKeyId != currentKeyId || 1 != len(keysById) || encryptDevelopmentKey != string(keysById[encryptDevelopmentKeyId]) {
        t.Fatalf("expected the development key alone, got %q over %q", currentKeyId, keysById)
    }

    refusal := recoveredRefusal(t, func() { encryptKeys("  ", "ignored", false) })
    if nil == refusal || false == strings.Contains(refusal.Error(), environmentKeyEncryptKeys+" is required outside development") {
        t.Fatalf("expected a blank list refused outside development, got %v", refusal)
    }
}

/* the keys .env commits are public: outside development a list holding either refuses the boot naming the entry's position and id, never the key, while a list of keys of its own boots and development keeps the committed ones */
func TestEncryptKeys_RefusesACommittedKeyOutsideDevelopment(t *testing.T) {
    for _, rawKeyList := range []string{
        "example-2026:" + encryptDevelopmentKey,
        "own-1:an-operator-chosen-key-of-32-bytes,example-2027:" + encryptDevelopmentRotationKey,
    } {
        refusal := recoveredRefusal(t, func() { encryptKeys(rawKeyList, "example-2026", false) })
        if nil == refusal || false == strings.Contains(refusal.Error(), "holds the development value") {
            t.Fatalf("%q: expected the committed key refused outside development, got %v", rawKeyList, refusal)
        }

        logged := fmt.Sprintf("%v", melodyexception.LogContext(refusal))
        if true == strings.Contains(refusal.Error(), "cipher-key") || true == strings.Contains(logged, "cipher-key") {
            t.Fatalf("%q: the refusal names the key: %v %v", rawKeyList, refusal, logged)
        }

        if false == strings.Contains(logged, "keyId:") || false == strings.Contains(logged, "environmentKey:"+environmentKeyEncryptKeys) {
            t.Fatalf("%q: expected the refusal to name the key id and the environment key, got %v", rawKeyList, logged)
        }
    }

    if _, keysById := encryptKeys("own-1:an-operator-chosen-key-of-32-bytes", "own-1", false); 1 != len(keysById) {
        t.Fatalf("expected a key of its own to boot outside development, got %q", keysById)
    }

    if _, keysById := encryptKeys("example-2026:"+encryptDevelopmentKey, "example-2026", true); 1 != len(keysById) {
        t.Fatalf("expected development to keep the committed key, got %q", keysById)
    }
}

/* an entry with no separator may be the key itself — a bare key, or the rotation typo id:k,<key> whose second entry is the key — so its refusal carries the position alone; an entry with the separator carries its id, which names a key and is no secret */
func TestEncryptKeys_AnEntryWithoutTheSeparatorIsRefusedWithoutItsText(t *testing.T) {
    for _, rawKeyList := range []string{encryptDevelopmentKey, "id:k," + encryptDevelopmentKey} {
        refusal := recoveredRefusal(t, func() { encryptKeys(rawKeyList, "id", true) })
        if nil == refusal {
            t.Fatalf("%q: expected the list refused", rawKeyList)
        }

        logged := fmt.Sprintf("%v", melodyexception.LogContext(refusal))
        if true == strings.Contains(logged, encryptDevelopmentKey) || true == strings.Contains(logged, "keyId") {
            t.Fatalf("%q: expected the refusal to carry the position alone, got %v", rawKeyList, logged)
        }
    }

    refusal := recoveredRefusal(t, func() { encryptKeys("example-2026:", "example-2026", true) })
    if nil == refusal || false == strings.Contains(fmt.Sprintf("%v", melodyexception.LogContext(refusal)), "keyId:example-2026") {
        t.Fatalf("expected an entry with its separator to name its id, got %v", refusal)
    }
}

func TestEncryptKeys_RefusesAMalformedListNamingTheEntryAndNeverTheKey(t *testing.T) {
    for _, rawKeyList := range []string{"example-2026", "example-2026:", ":melody-example-cipher-key-32byte", "a:melody-example-cipher-key-32byte,a:melody-example-cipher-key-32byte", "melody-example-cipher-key-32byte", "id:k,melody-example-cipher-key-32byte"} {
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

            encryptKeys(rawKeyList, "example-2026", true)
        }()
    }
}
