package migration

import (
    "context"
    "database/sql/driver"
    "reflect"
    "strings"
    "testing"
)

func TestExpectedSchemaOf_ReadsTheColumnsTheSetCreates(t *testing.T) {
    catalogue := expectedSchemaOf(schemaUpStatementList)
    if 10 != len(catalogue) {
        t.Fatalf("expected the nine tables of the catalogue and its fingerprint table, got %d: %v", len(catalogue), catalogue)
    }

    columnListByTable := map[string][]string{}
    for _, table := range catalogue {
        columnListByTable[table.name] = table.columnNameList
    }

    wanted := map[string][]string{
        "melody_example_v4_currency":   {"id", "code", "name", "rate", "rate_as_of", "provider_rate_as_of"},
        "melody_example_v4_two_factor": {"user_identifier", "secret", "recovery_codes", "created_at"},
        "melody_example_v4_two_factor_pending": {"user_identifier", "secret", "recovery_codes", "replaces_factor", "created_at"},
        "melody_example_v4_category":   {"id", "name"},
        SchemaFingerprintTableName:     {"set_name", "fingerprint", "state"},
    }
    for tableName, columnNameList := range wanted {
        if false == reflect.DeepEqual(columnNameList, columnListByTable[tableName]) {
            t.Errorf("%s: expected %v, got %v", tableName, columnNameList, columnListByTable[tableName])
        }
    }

    archive := expectedSchemaOf(archiveUpStatementList)
    if 2 != len(archive) || false == reflect.DeepEqual([]string{"taken_at", "headline", "payload", "product_count", "journal_count"}, archive[0].columnNameList) || ArchiveSchemaFingerprintTableName != archive[1].name || false == reflect.DeepEqual([]string{"set_name", "fingerprint", "state"}, archive[1].columnNameList) {
        t.Fatalf("expected the archive's table with its five columns and its fingerprint table, got %v", archive)
    }
}

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

func TestEnsureMigratedRefusesAVolumeThatLacksAColumnAndIsNotRememberedForIt(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = volumeAnswering("melody_example_v4_currency", []string{"provider_rate_as_of"}, nil)

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr {
        t.Fatal("a volume lacking a column the code reads was taken for the present schema")
    }

    for _, wanted := range []string{"melody_example_v4_currency lacks provider_rate_as_of", "run example:db:reset --force"} {
        if false == strings.Contains(ensureErr.Error(), wanted) {
            t.Errorf("the refusal does not say %q: %v", wanted, ensureErr)
        }
    }

    recorder.queryHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("the resolution after the volume was brought here was refused: %v", retryErr)
    }
}

func TestEnsureMigratedRefusesAVolumeThatCarriesAColumnTheSetDoesNotDeclare(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = volumeAnswering("melody_example_v4_user", nil, []string{"legacy_email"})

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr || false == strings.Contains(ensureErr.Error(), "melody_example_v4_user carries legacy_email") {
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

func TestExpectedSchemaOf_KeepsAQuotedCommaInsideItsItem(t *testing.T) {
    table, isCreate := expectedTableOf("CREATE TABLE IF NOT EXISTS `probe` (`a` INT COMMENT 'one, (two', `b` VARCHAR(8) DEFAULT \"x,y\", PRIMARY KEY (`a`))")

    if false == isCreate || false == reflect.DeepEqual([]string{"a", "b"}, table.columnNameList) {
        t.Fatalf("expected the columns a and b, got %v", table.columnNameList)
    }
}

/* a backslash escapes the next character inside a string literal, so `it\'s` does not close it */
func TestExpectedSchemaOf_ReadsABackslashEscapedQuoteInsideItsLiteral(t *testing.T) {
    table, isCreate := expectedTableOf("CREATE TABLE IF NOT EXISTS `probe` (`a` INT COMMENT 'it\\'s, x', `b` VARCHAR(8) DEFAULT \"a\\\",b\", `c\\` INT, `d` INT, PRIMARY KEY (`a`))")

    if false == isCreate || false == reflect.DeepEqual([]string{"a", "b", "c\\", "d"}, table.columnNameList) {
        t.Fatalf("expected the columns a, b, c\\ and d, got %v", table.columnNameList)
    }
}
