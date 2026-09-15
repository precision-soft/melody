package repository

import (
    "errors"
    "fmt"
    "strings"
    "testing"
    "github.com/precision-soft/melody/v3/.example/migration"
)

func TestCatalogReadingRowNamesTheTableTheMigrationCreates(t *testing.T) {
    database := newRenderingDatabase()

    rendered := database.NewSelect().Model((*catalogReadingRow)(nil)).String()

    if false == strings.Contains(rendered, migration.CatalogReadingTableName) {
        t.Fatalf("the row does not read the table the migration creates (%s); rendered: %s", migration.CatalogReadingTableName, rendered)
    }
}

func TestAsReadingAlreadyRecordedMapsTheArchivesOwnConflict(t *testing.T) {
    mapped := asReadingAlreadyRecorded(&sqlStateError{state: "23505"})
    if nil == mapped {
        t.Fatal("expected a duplicate key to be mapped, got nil")
    }

    if "reading already recorded" != mapped.Error() {
        t.Fatalf("expected the archive's own sentence, got %q", mapped.Error())
    }
}

func TestAsReadingAlreadyRecordedSeesThroughWrapping(t *testing.T) {
    wrapped := fmt.Errorf("inserting the reading: %w", &sqlStateError{state: "23505"})

    mapped := asReadingAlreadyRecorded(wrapped)
    if nil == mapped || "reading already recorded" != mapped.Error() {
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
