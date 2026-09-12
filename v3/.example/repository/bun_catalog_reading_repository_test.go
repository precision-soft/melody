package repository

import (
    "errors"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/.example/migration"
)

/* the row's table name is a struct TAG, so it cannot read the constant the migration owns — a tag is a literal. This is the only thing keeping the two spellings together, which is why it exists and why the comment on the row points at it by name. */
func TestCatalogReadingRowNamesTheTableTheMigrationCreates(t *testing.T) {
    database := newRenderingDatabase()

    rendered := database.NewSelect().Model((*catalogReadingRow)(nil)).String()

    if false == strings.Contains(rendered, migration.CatalogReadingTableName) {
        t.Fatalf("the row does not read the table the migration creates (%s); rendered: %s", migration.CatalogReadingTableName, rendered)
    }
}

/* sqlStateError is the shape pgx and lib/pq errors carry: the SQLSTATE through a SQLState() method. It is the door pgsql.IsDuplicateKey was widened to read, so mapping through the door rather than through the text of the message is what this pins. */
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

    if "reading already recorded" != mapped.Error() {
        t.Fatalf("expected the archive's own sentence, got %q", mapped.Error())
    }
}

/* the mapping reads the TYPED sqlstate through errors.As, so it sees a conflict through the wrapping an exception puts around it — which a probe of the message text could not. */
func TestAsReadingAlreadyRecordedSeesThroughWrapping(t *testing.T) {
    wrapped := fmt.Errorf("inserting the reading: %w", &sqlStateError{state: "23505"})

    mapped := asReadingAlreadyRecorded(wrapped)
    if nil == mapped || "reading already recorded" != mapped.Error() {
        t.Fatalf("expected a wrapped duplicate to be mapped, got %v", mapped)
    }
}

/* every other failure is handed back untouched, so a connection that dropped mid-insert stays the diagnosis it is rather than being reported as a reading that was already there. A NOT NULL violation is the case that matters: it is a constraint error from the same server, so a mapper that keyed on "an error from postgres" rather than on the state would answer wrongly. */
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
