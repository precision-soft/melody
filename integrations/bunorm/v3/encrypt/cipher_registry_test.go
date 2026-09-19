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

func TestUseCipher_NilResetsTheEntry(t *testing.T) {
    UseCipher(NewFakeCipher())
    UseCipher(nil)

    if _, cipherErr := cipherByName(defaultCipherName); nil == cipherErr {
        t.Fatalf("expected the default entry to be reset")
    }
}

/* a bare nil is the documented deinstall door; a TYPED nil is a failed resolution installed anyway, and stored it would be handed out with a nil error and dereferenced inside database/sql at the first column write */
func TestUseCipher_RefusesATypedNilCipher(t *testing.T) {
    defer func() {
        recovered := recover()
        if nil == recovered {
            t.Fatalf("expected the typed-nil cipher to be refused at the door")
        }

        if false == strings.Contains(fmt.Sprintf("%v", recovered), "typed nil") {
            t.Fatalf("expected the panic to name the typed nil, got %v", recovered)
        }
    }()

    var cipherInstance *fakeCipher

    UseCipher(cipherInstance)
}
