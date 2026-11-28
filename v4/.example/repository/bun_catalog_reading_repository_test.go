package repository

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v4/.example/migration"
)

func TestCatalogReadingRowNamesTheTableTheMigrationCreates(t *testing.T) {
    database := newRenderingDatabase()

    rendered := database.NewSelect().Model((*catalogReadingRow)(nil)).String()

    if false == strings.Contains(rendered, migration.CatalogReadingTableName) {
        t.Fatalf("the row does not read the table the migration creates (%s); rendered: %s", migration.CatalogReadingTableName, rendered)
    }
}

type sqlStateError struct {
    state string
}

func (instance *sqlStateError) Error() string {
    return "some server error"
}

func (instance *sqlStateError) SQLState() string {
    return instance.state
}

func TestAsReadingAlreadyRecordedMapsTheArchivesOwnConflict(t *testing.T) {
    mapped := asReadingAlreadyRecorded(&sqlStateError{state: "23505"})
    if nil == mapped {
        t.Fatal("expected a duplicate key to be mapped, got nil")
    }

    if false == errors.Is(mapped, ErrReadingAlreadyRecorded) {
        t.Fatalf("expected ErrReadingAlreadyRecorded, got %q", mapped.Error())
    }
}

func TestAsReadingAlreadyRecordedSeesThroughWrapping(t *testing.T) {
    wrapped := fmt.Errorf("inserting the reading: %w", &sqlStateError{state: "23505"})

    mapped := asReadingAlreadyRecorded(wrapped)
    if false == errors.Is(mapped, ErrReadingAlreadyRecorded) {
        t.Fatalf("expected a wrapped duplicate to be mapped, got %v", mapped)
    }
}

func TestAsReadingAlreadyRecordedHandsBackEveryOtherFailure(t *testing.T) {
    notNullViolation := &sqlStateError{state: "23502"}

    mapped := asReadingAlreadyRecorded(notNullViolation)
    if false == errors.Is(mapped, error(notNullViolation)) {
        t.Fatalf("expected the original failure to be handed back, got %v", mapped)
    }

    if nil != asReadingAlreadyRecorded(nil) {
        t.Fatal("expected a nil write error to stay nil")
    }
}
