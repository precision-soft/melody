package bunorm

import (
    "context"
    "database/sql"
    "errors"
    "strings"
    "testing"
    "time"

    "github.com/uptrace/bun"
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

    firstErr := manager.CloseWithContext(t.Context())
    if false == errors.Is(firstErr, closeRefused) {
        t.Fatalf("expected the underlying close error, got %v", firstErr)
    }

    secondErr := manager.CloseWithContext(t.Context())
    if false == errors.Is(secondErr, closeRefused) {
        t.Fatalf("expected the memoized close error on the second call, got %v", secondErr)
    }
}

func TestManager_CloseTreatsANilDatabaseAsClean(t *testing.T) {
    manager := NewManager("main", nil)

    if closeErr := manager.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("expected a clean close for a nil database, got %v", closeErr)
    }
}

func TestManager_ADatabaseCloseOutlivingTheDeadlineAnswersTheDeadlineAndFinishesInTheBackground(t *testing.T) {
    database, closeEntered, releaseClose := newBlockingCloseDatabase()
    manager := NewManager("main", database)

    closeContext, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
    defer cancel()

    closeErr := manager.CloseWithContext(closeContext)
    if false == errors.Is(closeErr, context.DeadlineExceeded) {
        t.Fatalf("expected the deadline's error over a close still running, got %v", closeErr)
    }

    <-closeEntered
    close(releaseClose)

    if laterErr := manager.CloseWithContext(t.Context()); nil != laterErr {
        t.Fatalf("expected the finished close to answer nil, got %v", laterErr)
    }
}

type panickingCloseConnectorForManager struct {
    blockingCloseConnector
}

func (instance *panickingCloseConnectorForManager) Close() error {
    panic(errors.New("the driver tore its own state down"))
}

func TestManager_APanickingDatabaseCloseIsAnsweredAsTheClosesErrorEveryTime(t *testing.T) {
    database := bun.NewDB(sql.OpenDB(&panickingCloseConnectorForManager{}), newCloseRaceDialect())
    manager := NewManager("main", database)

    for attempt := 0; attempt < 2; attempt++ {
        closeErr := manager.CloseWithContext(t.Context())
        if nil == closeErr || false == strings.Contains(closeErr.Error(), "panicked") {
            t.Fatalf("attempt %d: expected the panic answered as the close's error, got %v", attempt, closeErr)
        }
    }
}
