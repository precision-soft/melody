package migration

import (
    "context"
    "database/sql/driver"
    "reflect"
    "strings"
    "testing"
)

/* the expected schema is read out of the set's own statements: every column in the order written, no constraint
   taken for a column, and no table for the statement that only adds the username key */
func TestExpectedSchemaOf_ReadsTheColumnsTheSetCreates(t *testing.T) {
    catalogue := expectedSchemaOf(schemaUpStatementList)
    if 6 != len(catalogue) {
        t.Fatalf("expected the six tables of the catalogue, got %d: %v", len(catalogue), catalogue)
    }

    columnListByTable := map[string][]string{}
    for _, table := range catalogue {
        columnListByTable[table.name] = table.columnNameList
    }

    wanted := map[string][]string{
        "melody_example_v3_currency":   {"id", "code", "name", "rate", "rate_as_of", "provider_rate_as_of"},
        "melody_example_v3_two_factor": {"user_identifier", "secret", "recovery_codes", "created_at"},
        "melody_example_v3_category":   {"id", "name"},
    }
    for tableName, columnNameList := range wanted {
        if false == reflect.DeepEqual(columnNameList, columnListByTable[tableName]) {
            t.Errorf("%s: expected %v, got %v", tableName, columnNameList, columnListByTable[tableName])
        }
    }

    archive := expectedSchemaOf(archiveUpStatementList)
    if 1 != len(archive) || false == reflect.DeepEqual([]string{"taken_at", "headline", "payload", "product_count", "journal_count"}, archive[0].columnNameList) {
        t.Fatalf("expected the archive's one table with its five columns, got %v", archive)
    }
}

/* volumeAnswering answers the information schema as a volume in another shape would: the columns the set creates,
   less the ones dropped, plus the ones added, for the one table named */
func volumeAnswering(tableName string, dropped []string, added []string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if false == strings.Contains(query, "information_schema.columns") || false == strings.Contains(query, "'"+tableName+"'") {
            return nil, nil, nil
        }

        columnNameList, _ := presentColumnNameListAsked(query)

        var rows [][]driver.Value
        for _, columnName := range columnNameList {
            isDropped := false
            for _, droppedName := range dropped {
                isDropped = isDropped || droppedName == columnName
            }

            if false == isDropped {
                rows = append(rows, []driver.Value{columnName})
            }
        }

        for _, addedName := range added {
            rows = append(rows, []driver.Value{addedName})
        }

        return []string{"column_name"}, rows, nil
    }
}

/* a volume provisioned before a column was added passes the set untouched — the set is recorded as applied by name
   and its tables are created IF NOT EXISTS — and every request reaching the column answered 500 on "Unknown column";
   the first resolution now refuses the volume, naming the table, the column and the door that brings it here, and
   the refusal is not remembered: the resolution after the reset goes on */
func TestEnsureMigratedRefusesAVolumeThatLacksAColumnAndIsNotRememberedForIt(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = volumeAnswering("melody_example_v3_currency", []string{"provider_rate_as_of"}, nil)

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr {
        t.Fatal("a volume lacking a column the code reads was taken for the present schema")
    }

    for _, wanted := range []string{"melody_example_v3_currency lacks provider_rate_as_of", "run example:db:reset --force"} {
        if false == strings.Contains(ensureErr.Error(), wanted) {
            t.Errorf("the refusal does not say %q: %v", wanted, ensureErr)
        }
    }

    recorder.queryHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("the resolution after the volume was brought here was refused: %v", retryErr)
    }
}

/* a column no statement declares is another shape too — a volume left by a schema this code no longer writes */
func TestEnsureMigratedRefusesAVolumeThatCarriesAColumnTheSetDoesNotDeclare(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = volumeAnswering("melody_example_v3_user", nil, []string{"legacy_email"})

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr || false == strings.Contains(ensureErr.Error(), "melody_example_v3_user carries legacy_email") {
        t.Fatalf("expected the unexpected column named, got %v", ensureErr)
    }
}

func TestEnsureArchiveMigratedRefusesAnArchiveInAnotherShape(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = volumeAnswering(CatalogReadingTableName, []string{"journal_count"}, nil)

    ensureErr := EnsureArchiveMigrated(context.Background(), database)
    if nil == ensureErr || false == strings.Contains(ensureErr.Error(), "the archive set finds the volume in another shape") || false == strings.Contains(ensureErr.Error(), "lacks journal_count") {
        t.Fatalf("expected the archive's drift named, got %v", ensureErr)
    }
}
