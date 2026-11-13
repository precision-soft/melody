package encrypt

import (
    "fmt"
    "strings"
    "testing"
)

func assertPanics(t *testing.T, callback func()) {
    t.Helper()

    defer func() {
        if recovered := recover(); nil == recovered {
            t.Fatalf("expected a panic")
        }
    }()

    callback()
}

func TestCipherByName_UnconfiguredNamesError(t *testing.T) {
    if _, cipherErr := cipherByName("missing"); nil == cipherErr || false == strings.Contains(cipherErr.Error(), "no cipher is installed under this name; register the encrypt module with a context of this name and its Cipher") {
        t.Fatalf("expected the named refusal to name the module context, got %v", cipherErr)
    }

    if _, cipherErr := cipherByName(defaultCipherName); nil == cipherErr {
        t.Fatalf("expected an error for the unconfigured default cipher")
    }
}

func TestCipherRegistry_NamedAndDefaultEntriesAreIndependent(t *testing.T) {
    defaultCipher := NewFakeCipher()
    namedCipher := NewFakeCipher()

    installCipher(t, defaultCipherName, defaultCipher)
    installCipher(t, "crm", namedCipher)

    /* the two assertions below can only distinguish the compartments while the two fakes are distinguishable themselves; asserted first, so a fake that collapsed back into one value fails here instead of leaving both of them holding whichever entry the registry answered from */
    if defaultCipher == namedCipher {
        t.Fatalf("the two fakes are the same value, so neither assertion below can fail")
    }

    resolvedDefault, defaultErr := cipherByName(defaultCipherName)
    if nil != defaultErr || defaultCipher != resolvedDefault {
        t.Fatalf("expected the default entry: %v %v", resolvedDefault, defaultErr)
    }

    resolvedNamed, namedErr := cipherByName("crm")
    if nil != namedErr || namedCipher != resolvedNamed {
        t.Fatalf("expected the named entry: %v %v", resolvedNamed, namedErr)
    }
}

func TestStoreCipher_RefusesATypedNilCipher(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the typed-nil cipher to be refused")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "cipher instance is nil") {
            t.Fatalf("expected the panic to name the nil cipher, got %v", recovered)
        }
    }()

    var cipherInstance *fakeCipher

    storeCipher(defaultCipherName, cipherInstance)
}

func TestStoreCipher_RefusesABareNilCipher(t *testing.T) {
    assertPanics(t, func() {
        storeCipher(defaultCipherName, nil)
    })
}

func TestStoreCipher_ASecondInstallReplacesTheFirst(t *testing.T) {
    first := NewFakeCipher()
    second := NewFakeCipher()

    installCipher(t, defaultCipherName, first)
    storeCipher(defaultCipherName, second)

    resolved, resolveErr := cipherByName(defaultCipherName)
    if nil != resolveErr || second != resolved {
        t.Fatalf("expected the second cipher: %v %v", resolved, resolveErr)
    }
}
