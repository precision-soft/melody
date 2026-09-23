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
