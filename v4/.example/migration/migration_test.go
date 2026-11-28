package migration

import (
    "context"
    "strings"
    "testing"
)

func TestMigrationsHoldOneSchemaMigration(t *testing.T) {
    sorted := Migrations.Sorted()
    if 1 != len(sorted) {
        t.Fatalf("expected the set to hold one migration, got %d", len(sorted))
    }

    if "20260907000001" != sorted[0].Name {
        t.Fatalf("expected the migration to be 20260907000001, got %s", sorted[0].Name)
    }
    if nil == sorted[0].Up || nil == sorted[0].Down {
        t.Fatalf("expected migration %s to carry both directions", sorted[0].Name)
    }
}

func TestUpSchemaCreatesEveryTableTolerantly(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    assertQueryOrder(t, recorder.recordedQueries(), []string{
        "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN ('melody_example_v4_schema_fingerprint', 'melody_example_v4_two_factor_pending', 'melody_example_v4_two_factor', 'melody_example_v4_catalog_journal', 'melody_example_v4_identifier_sequence', 'melody_example_v4_user_session', 'melody_example_v4_user', 'melody_example_v4_product', 'melody_example_v4_currency', 'melody_example_v4_category')",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_schema_fingerprint`",
        "INSERT INTO `melody_example_v4_schema_fingerprint` (`set_name`, `fingerprint`, `state`) VALUES ('catalogue', '" + catalogueSchemaFingerprint + "', 'building')",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_category`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_currency`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_product`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_user`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_user_session`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_identifier_sequence`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_catalog_journal`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_two_factor`",
        "CREATE TABLE IF NOT EXISTS `melody_example_v4_two_factor_pending`",
        "UPDATE `melody_example_v4_schema_fingerprint` SET `state` = 'built' WHERE `set_name` = 'catalogue'",
    })
}

func TestUpSchemaHoldsAProductsReferences(t *testing.T) {
    for _, wanted := range []string{
        "CONSTRAINT `" + ProductCurrencyForeignKeyName + "` FOREIGN KEY (`currency_id`) REFERENCES `melody_example_v4_currency` (`id`) ON DELETE RESTRICT",
        "CONSTRAINT `" + ProductCategoryForeignKeyName + "` FOREIGN KEY (`category_id`) REFERENCES `melody_example_v4_category` (`id`) ON DELETE RESTRICT",
    } {
        if false == strings.Contains(createProductTableSql, wanted) {
            t.Fatalf("expected the product table to declare %s, got %s", wanted, createProductTableSql)
        }
    }
}

func TestUpSchemaHoldsTheFoldedUsernameUnique(t *testing.T) {
    for _, wanted := range []string{
        "`username_normalized` VARCHAR(255) COLLATE utf8mb4_bin NOT NULL",
        "UNIQUE KEY `" + UserUsernameIndexName + "` (`username_normalized`)",
    } {
        if false == strings.Contains(createUserTableSql, wanted) {
            t.Fatalf("expected the user table to declare %s, got %s", wanted, createUserTableSql)
        }
    }
}

func TestUpSchemaTiesASessionRowToItsAccount(t *testing.T) {
    if false == strings.Contains(createUserSessionTableSql, "FOREIGN KEY (`user_identifier`) REFERENCES `melody_example_v4_user` (`id`) ON DELETE CASCADE") {
        t.Fatalf("expected the session index to cascade with the account, got %s", createUserSessionTableSql)
    }
}

func TestUpSchemaTiesATwoFactorRowToItsAccount(t *testing.T) {
    if false == strings.Contains(createTwoFactorTableSql, "FOREIGN KEY (`user_identifier`) REFERENCES `melody_example_v4_user` (`id`) ON DELETE CASCADE") {
        t.Fatalf("expected the two-factor table to cascade its rows with the account, got %s", createTwoFactorTableSql)
    }

    userIndex, twoFactorIndex := -1, -1
    for index, statement := range schemaUpStatementList {
        if true == strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS `melody_example_v4_user`") {
            userIndex = index
        }
        if true == strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS `melody_example_v4_two_factor`") {
            twoFactorIndex = index
        }
    }
    if -1 == userIndex || -1 == twoFactorIndex || twoFactorIndex < userIndex {
        t.Fatalf("expected the user table created before the two-factor table that references it, got user at %d and two-factor at %d", userIndex, twoFactorIndex)
    }
}

func TestDownSchemaDropsTheTablesInReverse(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if downErr := downSchema(context.Background(), database); nil != downErr {
        t.Fatalf("expected the down migration to succeed, got %v", downErr)
    }

    assertQueryOrder(t, recorder.recordedQueries(), []string{
        "DROP TABLE IF EXISTS `melody_example_v4_schema_fingerprint`",
        "DROP TABLE IF EXISTS `melody_example_v4_two_factor_pending`",
        "DROP TABLE IF EXISTS `melody_example_v4_two_factor`",
        "DROP TABLE IF EXISTS `melody_example_v4_catalog_journal`",
        "DROP TABLE IF EXISTS `melody_example_v4_identifier_sequence`",
        "DROP TABLE IF EXISTS `melody_example_v4_user_session`",
        "DROP TABLE IF EXISTS `melody_example_v4_user`",
        "DROP TABLE IF EXISTS `melody_example_v4_product`",
        "DROP TABLE IF EXISTS `melody_example_v4_currency`",
        "DROP TABLE IF EXISTS `melody_example_v4_category`",
    })
}

func TestUpSchemaComparesEveryIdentifierColumnByteForByte(t *testing.T) {
    database, recorder := newFakeBunDatabase()

    if upErr := upSchema(context.Background(), database); nil != upErr {
        t.Fatalf("expected the up migration to succeed, got %v", upErr)
    }

    rendered := strings.Join(recorder.recordedQueries(), "\n")

    identifierColumnList := []string{"`id`", "`category_id`", "`currency_id`", "`user_identifier`"}
    collated := 0
    for _, column := range identifierColumnList {
        collated += strings.Count(rendered, column+" VARCHAR(255) COLLATE utf8mb4_bin NOT NULL")

        if uncollated := strings.Count(rendered, column+" VARCHAR(255) NOT NULL"); 0 != uncollated {
            t.Errorf("%s is declared %d time(s) under the table's default collation", column, uncollated)
        }
    }

    if 9 != collated {
        t.Errorf("%d identifier columns are compared under utf8mb4_bin, wanted 9", collated)
    }
}
