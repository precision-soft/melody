package encrypt

import (
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

func TestUseCipherNamed_RejectsEmptyName(t *testing.T) {
    assertPanics(t, func() {
        UseCipherNamed("", NewFakeCipher())
    })
}

func TestCipherByName_UnconfiguredNamesError(t *testing.T) {
    if _, cipherErr := cipherByName("missing"); nil == cipherErr {
        t.Fatalf("expected an error for an unconfigured named cipher")
    }

    if _, cipherErr := cipherByName(defaultCipherName); nil == cipherErr {
        t.Fatalf("expected an error for the unconfigured default cipher")
    }
}

func TestCipherRegistry_NamedAndDefaultEntriesAreIndependent(t *testing.T) {
    defaultCipher := NewFakeCipher()
    namedCipher := NewFakeCipher()

    UseCipher(defaultCipher)
    defer UseCipher(nil)

    UseCipherNamed("crm", namedCipher)
    defer UseCipherNamed("crm", NewFakeCipher())
    defer func() {
        storeCipher("crm", nil)
    }()

    resolvedDefault, defaultErr := cipherByName(defaultCipherName)
    if nil != defaultErr || defaultCipher != resolvedDefault {
        t.Fatalf("expected the default entry: %v %v", resolvedDefault, defaultErr)
    }

    resolvedNamed, namedErr := cipherByName("crm")
    if nil != namedErr || namedCipher != resolvedNamed {
        t.Fatalf("expected the named entry: %v %v", resolvedNamed, namedErr)
    }
}

func TestUseCipher_NilResetsTheEntry(t *testing.T) {
    UseCipher(NewFakeCipher())
    UseCipher(nil)

    if _, cipherErr := cipherByName(defaultCipherName); nil == cipherErr {
        t.Fatalf("expected the default entry to be reset")
    }
}
