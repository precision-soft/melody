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

func TestManager_CloseTreatsANilDatabaseAsClean(t *testing.T) {
    manager := NewManager("main", nil)

    if closeErr := manager.Close(); nil != closeErr {
        t.Fatalf("expected a clean close for a nil database, got %v", closeErr)
    }
}
