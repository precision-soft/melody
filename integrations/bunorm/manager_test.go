package bunorm

import (
    "errors"
    "testing"
)

func TestManager_ExposesDefinitionNameAndDatabase(t *testing.T) {
    database, _ := newCloseRaceDatabase()

    manager := NewManager("main", database)

    if "main" != manager.DefinitionName() {
        t.Fatalf("expected the definition name, got %q", manager.DefinitionName())
    }

    if database != manager.Database() {
        t.Fatalf("expected the wrapped database")
    }
}

/* @info Close collapses to one underlying close and memoizes its verdict: the registry and a container that also holds the manager both close it, and the second caller must receive the same answer without a second close reaching the pool */
func TestManager_CloseIsOnceAndMemoizesItsError(t *testing.T) {
    closeRefused := errors.New("close refused")

    manager := NewManager("main", newFailClosingDatabase(closeRefused))

    firstErr := manager.Close()
    if false == errors.Is(firstErr, closeRefused) {
        t.Fatalf("expected the underlying close error, got %v", firstErr)
    }

    secondErr := manager.Close()
    if false == errors.Is(secondErr, closeRefused) {
        t.Fatalf("expected the memoized close error on the second call, got %v", secondErr)
    }
}

/* @info a manager over a nil database reports a clean close instead of dereferencing it */
func TestManager_CloseTreatsANilDatabaseAsClean(t *testing.T) {
    manager := NewManager("main", nil)

    if closeErr := manager.Close(); nil != closeErr {
        t.Fatalf("expected a clean close for a nil database, got %v", closeErr)
    }
}
