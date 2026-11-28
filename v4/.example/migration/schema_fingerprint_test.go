package migration

import (
    "context"
    "database/sql/driver"
    "strings"
    "testing"
)

func fingerprintAnswering(heldFingerprint string) func(query string) ([]string, [][]driver.Value, error) {
    return recordAnswering(heldFingerprint, schemaSetBuilt)
}

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

    if schemaFingerprintOf(schemaUpStatementList...) != catalogueSchemaFingerprint {
        t.Error("the catalogue's fingerprint is not the one of the statements the set runs")
    }
}

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

func TestUpSchemaRefusesToAdoptTablesItDidNotBuild(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = tablesAnswering("melody_example_v4_user", "melody_example_v4_currency")

    upErr := upSchema(context.Background(), database)
    if nil == upErr || false == strings.Contains(upErr.Error(), "the catalogue set is not recorded on this volume but finds its tables already there (melody_example_v4_currency, melody_example_v4_user)") || false == strings.Contains(upErr.Error(), schemaResetCommand) {
        t.Fatalf("expected the set to refuse the tables it found by name with the reset, got %v", upErr)
    }

    recordedList := recorder.recordedQueries()
    if 1 != len(recordedList) || false == strings.Contains(recordedList[0], "table_schema = DATABASE()") {
        t.Fatalf("expected the one read of the catalogue's database and nothing written, got %q", recordedList)
    }
}

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

func TestUpSchemaFinishesARunItBeganAndDidNotFinish(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering(catalogueSchemaFingerprint, schemaSetBuilding, SchemaFingerprintTableName, "melody_example_v4_category", "melody_example_v4_currency")

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the set to finish the run it began, got %v", upErr)
    }

    recordedList := recorder.recordedQueries()
    if 0 != countRecorded(recordedList, "INSERT INTO `"+SchemaFingerprintTableName+"`") {
        t.Fatalf("expected the row already building not to be written again, got %q", recordedList)
    }
    if 9 != countRecorded(recordedList, "CREATE TABLE IF NOT EXISTS `melody_example_v4_") {
        t.Fatalf("expected the nine tables of the set created again, tolerantly, got %q", recordedList)
    }
    if "UPDATE `"+SchemaFingerprintTableName+"` SET `state` = 'built' WHERE `set_name` = 'catalogue'" != recordedList[len(recordedList)-1] {
        t.Fatalf("expected the run to end by sealing the row, got %q", recordedList)
    }
}

func TestUpSchemaRefusesAnUnfinishedBuildOfOtherStatements(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", schemaSetBuilding, SchemaFingerprintTableName, "melody_example_v4_user")

    upErr := upSchema(context.Background(), database)
    if nil == upErr || false == strings.Contains(upErr.Error(), "the catalogue set finds an unfinished build of another schema on this volume (0123456789ab, tables melody_example_v4_schema_fingerprint, melody_example_v4_user)") || false == strings.Contains(upErr.Error(), schemaResetCommand) {
        t.Fatalf("expected the unfinished build of other statements refused by name with the reset, got %v", upErr)
    }

    if 0 != countRecorded(recorder.recordedQueries(), "CREATE TABLE") {
        t.Fatalf("expected nothing written after the refusal, got %q", recorder.recordedQueries())
    }
}

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

func TestCurrencyTableHoldsTheCodeUnique(t *testing.T) {
    if false == strings.Contains(createCurrencyTableSql, "UNIQUE KEY `"+CurrencyCodeIndexName+"` (`code`)") {
        t.Fatalf("the currency table declares no unique key on the code: %s", createCurrencyTableSql)
    }

    if false == strings.Contains(createCurrencyTableSql, "`code` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL") {
        t.Fatalf("the code is not compared byte for byte: %s", createCurrencyTableSql)
    }
}

func TestUpSchemaAnswersAVolumeItBuiltAndSealedWithoutRunningAStatement(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering(catalogueSchemaFingerprint, schemaSetBuilt, schemaTableNameList...)

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the sealed volume answered as built, got %v", upErr)
    }

    recordedList := recorder.recordedQueries()
    for _, fragment := range []string{"CREATE TABLE", "INSERT INTO `" + SchemaFingerprintTableName + "`", "UPDATE `" + SchemaFingerprintTableName + "`"} {
        if 0 != countRecorded(recordedList, fragment) {
            t.Fatalf("expected no statement over the sealed volume, got %q", recordedList)
        }
    }
}

func TestUpSchemaRefusesASealedVolumeMissingATableOrSealedUnderAnotherSchema(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering(catalogueSchemaFingerprint, schemaSetBuilt, schemaTableNameList[:len(schemaTableNameList)-1]...)

    if upErr := upSchema(context.Background(), database); nil == upErr || false == strings.Contains(upErr.Error(), schemaResetCommand) {
        t.Fatalf("expected a sealed volume missing a table refused with the reset, got %v", upErr)
    }

    database, recorder = newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", schemaSetBuilt, schemaTableNameList...)

    if upErr := upSchema(context.Background(), database); nil == upErr || false == strings.Contains(upErr.Error(), schemaResetCommand) {
        t.Fatalf("expected a volume sealed under another schema refused with the reset, got %v", upErr)
    }

    if 0 != countRecorded(recorder.recordedQueries(), "CREATE TABLE") {
        t.Fatalf("expected nothing written after the refusal, got %q", recorder.recordedQueries())
    }
}

func TestUpArchiveSchemaAnswersAVolumeItBuiltAndSealedWithoutRunningAStatement(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = interruptedVolumeAnswering(archiveSchemaFingerprint, schemaSetBuilt, archiveTableNameList...)

    if upErr := upArchiveSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the sealed archive volume answered as built, got %v", upErr)
    }

    if 0 != countRecorded(recorder.recordedQueries(), "CREATE TABLE") || 0 != countRecorded(recorder.recordedQueries(), "UPDATE "+ArchiveSchemaFingerprintTableName) {
        t.Fatalf("expected no statement over the sealed archive volume, got %q", recorder.recordedQueries())
    }
}
