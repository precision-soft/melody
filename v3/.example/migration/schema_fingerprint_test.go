package migration

import (
    "context"
    "database/sql/driver"
    "strings"
    "testing"
)

/* fingerprintAnswering answers the read of the fingerprint a set recorded with the one given, or with no row */
func fingerprintAnswering(heldFingerprint string) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if false == strings.HasPrefix(query, "SELECT fingerprint FROM ") {
            return nil, nil, nil
        }

        if "" == heldFingerprint {
            return []string{"fingerprint"}, nil, nil
        }

        return []string{"fingerprint"}, [][]driver.Value{{heldFingerprint}}, nil
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
