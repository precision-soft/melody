package migration

import (
    "context"
    "database/sql/driver"
    "strings"
    "testing"
)

/* fingerprintAnswering answers the read of the row a set recorded with the fingerprint given, sealed, or with no row */
func fingerprintAnswering(heldFingerprint string) func(query string) ([]string, [][]driver.Value, error) {
    return recordAnswering(heldFingerprint, schemaSetBuilt)
}

/* recordAnswering answers the read of the row a set recorded with the fingerprint and state given, or with no row */
func recordAnswering(heldFingerprint string, heldState string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if false == strings.HasPrefix(query, "SELECT fingerprint, state FROM ") {
            return nil, nil, nil
        }

        if "" == heldFingerprint {
            return []string{"fingerprint", "state"}, nil, nil
        }

        return []string{"fingerprint", "state"}, [][]driver.Value{{heldFingerprint, heldState}}, nil
    }
}

/* interruptedVolumeAnswering answers the read of the tables a set finds with the ones given and the read of its row as
   recordAnswering does, which is a volume a run left behind */
func interruptedVolumeAnswering(heldFingerprint string, heldState string, tableNameList ...string) func(query string) ([]string, [][]driver.Value, error) {
    tables := tablesAnswering(tableNameList...)
    record := recordAnswering(heldFingerprint, heldState)

    return func(query string) ([]string, [][]driver.Value, error) {
        if columns, rows, err := tables(query); 0 < len(columns) || nil != err {
            return columns, rows, err
        }

        return record(query)
    }
}

/* the columns of a volume built from other statements can be the same ones — a type, a collation, a key or a
   constraint changed under the same names — and the set, recorded as applied by name, passes it untouched; the
   fingerprint it recorded when it built the volume says so, and the refusal is not remembered */
func TestEnsureMigratedRefusesAVolumeBuiltFromAnotherSchemaAndIsNotRememberedForIt(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = fingerprintAnswering("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr {
        t.Fatal("a volume built from another schema was taken for the present one")
    }

    for _, wanted := range []string{"the catalogue set finds the volume built from another schema", "the volume holds 0123456789ab", "this code " + catalogueSchemaFingerprint[:12], "run example:db:reset --force"} {
        if false == strings.Contains(ensureErr.Error(), wanted) {
            t.Errorf("the refusal does not say %q: %v", wanted, ensureErr)
        }
    }

    recorder.queryHook = nil

    if retryErr := EnsureMigrated(context.Background(), database); nil != retryErr {
        t.Fatalf("the resolution after the volume was brought here was refused: %v", retryErr)
    }
}

/* a volume built before the set recorded a fingerprint holds none, and is refused the same way */
func TestEnsureMigratedRefusesAVolumeWithoutAFingerprint(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = fingerprintAnswering("")

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr || false == strings.Contains(ensureErr.Error(), "the volume holds none") {
        t.Fatalf("expected a volume without a fingerprint refused, got %v", ensureErr)
    }
}

func TestEnsureArchiveMigratedRefusesAnArchiveBuiltFromAnotherSchema(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = fingerprintAnswering(catalogueSchemaFingerprint)

    ensureErr := EnsureArchiveMigrated(context.Background(), database)
    if nil == ensureErr || false == strings.Contains(ensureErr.Error(), "the archive set finds the volume built from another schema") {
        t.Fatalf("expected the archive refused under the catalogue's fingerprint, got %v", ensureErr)
    }
}

/* the fingerprint moves with any statement, with their order, and with the constraint the catalogue adds outside
   its list of tables */
func TestSchemaFingerprintOf_MovesWithEveryStatement(t *testing.T) {
    original := schemaFingerprintOf("CREATE TABLE a (x INT)", "CREATE TABLE b (y INT)")

    for _, changed := range []string{
        schemaFingerprintOf("CREATE TABLE a (x BIGINT)", "CREATE TABLE b (y INT)"),
        schemaFingerprintOf("CREATE TABLE b (y INT)", "CREATE TABLE a (x INT)"),
        schemaFingerprintOf("CREATE TABLE a (x INT)CREATE TABLE b (y INT)"),
    } {
        if original == changed {
            t.Errorf("two different schemas share the fingerprint %s", original)
        }
    }

    if schemaFingerprintOf(schemaUpStatementList...) == catalogueSchemaFingerprint {
        t.Error("the catalogue's fingerprint does not cover the username key it adds after its tables")
    }
}

/* tablesAnswering answers the read of which of its tables a set finds with the ones given */
func tablesAnswering(presentList ...string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if false == strings.Contains(query, "information_schema.tables") {
            return nil, nil, nil
        }

        rows := make([][]driver.Value, 0, len(presentList))
        for _, tableName := range presentList {
            rows = append(rows, []driver.Value{tableName})
        }

        return []string{"table_name"}, rows, nil
    }
}

/* a set runs only on a volume that does not record it, and on one that already held its tables every CREATE ...
   IF NOT EXISTS was a no-op after which the set wrote this code's fingerprint over tables it did not build — the
   fingerprint then vouched for statements that never ran. The set refuses before it writes anything, naming the
   tables it found and the reset; asked of the catalogue's own database, in its own dialect */
func TestUpSchemaRefusesToAdoptTablesItDidNotBuild(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = tablesAnswering("melody_example_v3_user", "melody_example_v3_currency")

    upErr := upSchema(context.Background(), database)
    if nil == upErr || false == strings.Contains(upErr.Error(), "the catalogue set is not recorded on this volume but finds its tables already there (melody_example_v3_currency, melody_example_v3_user)") || false == strings.Contains(upErr.Error(), schemaResetCommand) {
        t.Fatalf("expected the set to refuse the tables it found by name with the reset, got %v", upErr)
    }

    recordedList := recorder.recordedQueries()
    if 1 != len(recordedList) || false == strings.Contains(recordedList[0], "table_schema = DATABASE()") {
        t.Fatalf("expected the one read of the catalogue's database and nothing written, got %q", recordedList)
    }
}

/* the archive set refuses the same way, before it writes anything */
func TestUpArchiveSchemaRefusesToAdoptTablesItDidNotBuild(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = tablesAnswering(CatalogReadingTableName)

    upErr := upArchiveSchema(context.Background(), database)
    if nil == upErr || false == strings.Contains(upErr.Error(), "the archive set is not recorded on this volume but finds its tables already there ("+CatalogReadingTableName+")") {
        t.Fatalf("expected the archive set to refuse the table it found by name, got %v", upErr)
    }

    if 1 != len(recorder.recordedQueries()) {
        t.Fatalf("expected nothing written after the refusal, got %q", recorder.recordedQueries())
    }
}

/* a run the set began and did not finish left its tables and its own row, still building under this code's
   fingerprint: the next run finishes it — the statements are IF NOT EXISTS — without writing the row again, and
   seals it */
func TestUpSchemaFinishesARunItBeganAndDidNotFinish(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering(catalogueSchemaFingerprint, schemaSetBuilding, SchemaFingerprintTableName, "melody_example_v3_category", "melody_example_v3_currency")

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the set to finish the run it began, got %v", upErr)
    }

    recordedList := recorder.recordedQueries()
    if 0 != countRecorded(recordedList, "INSERT INTO `"+SchemaFingerprintTableName+"`") {
        t.Fatalf("expected the row already building not to be written again, got %q", recordedList)
    }
    if 6 != countRecorded(recordedList, "CREATE TABLE IF NOT EXISTS `melody_example_v3_") {
        t.Fatalf("expected the six tables of the set created again, tolerantly, got %q", recordedList)
    }
    if "UPDATE `"+SchemaFingerprintTableName+"` SET `state` = 'built' WHERE `set_name` = 'catalogue'" != recordedList[len(recordedList)-1] {
        t.Fatalf("expected the run to end by sealing the row, got %q", recordedList)
    }
}

/* a row still building under ANOTHER fingerprint is a build of other statements: finishing it would seal this
   code's fingerprint over tables other statements began, so it is refused by name before anything is written */
func TestUpSchemaRefusesAnUnfinishedBuildOfOtherStatements(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", schemaSetBuilding, SchemaFingerprintTableName, "melody_example_v3_user")

    upErr := upSchema(context.Background(), database)
    if nil == upErr || false == strings.Contains(upErr.Error(), "the catalogue set finds an unfinished build of another schema on this volume (0123456789ab, tables melody_example_v3_schema_fingerprint, melody_example_v3_user)") || false == strings.Contains(upErr.Error(), schemaResetCommand) {
        t.Fatalf("expected the unfinished build of other statements refused by name with the reset, got %v", upErr)
    }

    if 0 != countRecorded(recorder.recordedQueries(), "CREATE TABLE") {
        t.Fatalf("expected nothing written after the refusal, got %q", recorder.recordedQueries())
    }
}

/* a record table standing alone and empty is a run that stopped between creating it and writing its row: nothing
   else was built, and the set starts over on it */
func TestUpSchemaStartsOverARecordTableStandingAloneAndEmpty(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering("", "", SchemaFingerprintTableName)

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the set to start over on an empty record table, got %v", upErr)
    }

    if 1 != countRecorded(recorder.recordedQueries(), "INSERT INTO `"+SchemaFingerprintTableName+"` (`set_name`, `fingerprint`, `state`) VALUES ('catalogue', '"+catalogueSchemaFingerprint+"', 'building')") {
        t.Fatalf("expected the row written as building before the set's statements, got %q", recorder.recordedQueries())
    }
}

/* the archive set finishes its own interrupted run the same way, in its own dialect */
func TestUpArchiveSchemaFinishesARunItBeganAndDidNotFinish(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering(archiveSchemaFingerprint, schemaSetBuilding, ArchiveSchemaFingerprintTableName, CatalogReadingTableName)

    if upErr := upArchiveSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the archive set to finish the run it began, got %v", upErr)
    }

    recordedList := recorder.recordedQueries()
    if "UPDATE "+ArchiveSchemaFingerprintTableName+" SET state = 'built' WHERE set_name = 'archive'" != recordedList[len(recordedList)-1] {
        t.Fatalf("expected the archive run to end by sealing its row, got %q", recordedList)
    }
}

/* a row still building vouches for nothing at the check: the set began on the volume and did not finish, and only
   a run that reaches its seal says every statement ran */
func TestEnsureMigratedRefusesAVolumeWhoseSetIsStillBuilding(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = recordAnswering(catalogueSchemaFingerprint, schemaSetBuilding)

    ensureErr := EnsureMigrated(context.Background(), database)
    if nil == ensureErr || false == strings.Contains(ensureErr.Error(), "holds an unfinished build of "+shortFingerprint(catalogueSchemaFingerprint)) {
        t.Fatalf("expected a set still building refused at the check, got %v", ensureErr)
    }
}

func countRecorded(recordedList []string, fragment string) int {
    count := 0
    for _, recorded := range recordedList {
        if true == strings.Contains(recorded, fragment) {
            count++
        }
    }

    return count
}
