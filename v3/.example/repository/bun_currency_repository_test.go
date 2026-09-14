package repository

import (
    "context"
    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/entity"
    "database/sql"
    "os"
    "strings"
    "testing"
    "time"
    _ "github.com/go-sql-driver/mysql"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)


/* Temporary tables shadow only this connection's example tables; permanent application rows are untouched. */
func TestBunIdentifiersAreExactOnMySQL(t *testing.T) {
    if "1" != os.Getenv("MELODY_EXAMPLE_SQL_ID_TEST") {
        t.Skip("requires the local example MySQL schema")
    }
    sqlDatabase, err := sql.Open("mysql", "melody:melody@tcp(mysql:3306)/melody_example_v3?parseTime=true")
    if nil != err { t.Fatal(err) }
    sqlDatabase.SetMaxOpenConns(1)
    database := bun.NewDB(sqlDatabase, mysqldialect.New())
    defer database.Close()
    ctx := context.Background()
    for _, kind := range []string{"user", "product", "category", "currency", "audit"} {
        table := "melody_example_v3_"+kind
        var tableName, definition string
        if err := sqlDatabase.QueryRowContext(ctx, "SHOW CREATE TABLE `"+table+"`").Scan(&tableName, &definition); nil != err { t.Fatal(err) }
        definition = strings.Replace(definition, "CREATE TABLE", "CREATE TEMPORARY TABLE", 1)
        if _, err := database.ExecContext(ctx, definition); nil != err { t.Fatal(err) }
    }
    user := seedUserList()[0]
    product := seedProductList(time.Now().UTC())[0]
    category := seedCategoryList()[0]
    currency := seedCurrencyList()[0]
    for _, row := range []any{newUserRow(user), newProductRow(product), newCategoryRow(category), newCurrencyRow(currency)} {
        if _, err := database.NewInsert().Model(row).Exec(ctx); nil != err { t.Fatal(err) }
    }
    for _, testCase := range []struct { name, id string; find func(string) (bool, error) }{
        {"user", user.Id, func(id string) (bool, error) { _, found, err := (&bunUserRepository{database: database}).FindById(ctx, id); return found, err }},
        {"product", product.Id, func(id string) (bool, error) { _, found, err := (&bunProductRepository{database: database}).FindById(ctx, id); return found, err }},
        {"category", category.Id, func(id string) (bool, error) { _, found, err := (&bunCategoryRepository{database: database}).FindById(ctx, id); return found, err }},
        {"currency", currency.Id, func(id string) (bool, error) { _, found, err := (&bunCurrencyRepository{database: database}).FindById(ctx, id); return found, err }},
    } {
        t.Run(testCase.name, func(t *testing.T) {
            if found, err := testCase.find(testCase.id); nil != err || false == found { t.Fatalf("canonical lookup: found=%v err=%v", found, err) }
            if found, err := testCase.find(strings.ToUpper(testCase.id)); nil != err || true == found { t.Fatalf("alias lookup: found=%v err=%v", found, err) }
        })
    }
    t.Run("atomic grant and audit", func(t *testing.T) {
        instance := newBunUserRepository(persistence.NewCatalogStorage(database))
        granted, changed, err := instance.GrantRole(ctx, user.Username, entity.RoleEditor)
        if nil != err || false == changed || nil == granted || user.Password != granted.Password { t.Fatalf("grant failed: account=%v changed=%v err=%v", granted, changed, err) }
        _, changed, err = instance.GrantRole(ctx, user.Username, entity.RoleEditor)
        if nil != err || true == changed { t.Fatalf("repeated grant wrote again: changed=%v err=%v", changed, err) }
        var count int
        if err := sqlDatabase.QueryRowContext(ctx, "SELECT COUNT(*) FROM melody_example_v3_audit").Scan(&count); nil != err { t.Fatal(err) }
        if 1 != count { t.Fatalf("audit entries=%d; want one", count) }
    })
    t.Run("currency instant round trip", func(t *testing.T) {
        stamp := time.Date(2026, 9, 7, 12, 0, 0, 0, time.FixedZone("provider", 3*60*60))
        currency.RateAsOf = stamp
        instance := &bunCurrencyRepository{database: database}
        if _, err := instance.Update(ctx, currency); nil != err { t.Fatal(err) }
        stored, _, err := instance.FindById(ctx, currency.Id)
        if nil != err || false == stamp.Equal(stored.RateAsOf) { t.Fatalf("instant moved in SQL: %v err=%v", stored, err) }
    })
    t.Run("identical currency update", func(t *testing.T) {
        found, err := (&bunCurrencyRepository{database: database}).Update(ctx, currency)
        if nil != err || false == found { t.Fatalf("unchanged row reported missing: found=%v err=%v", found, err) }
    })
    for _, testCase := range []struct { name, id string; remove func(context.Context, string) (bool, error) }{
        {"currency", currency.Id, (&bunCurrencyRepository{database: database}).DeleteById},
        {"category", category.Id, (&bunCategoryRepository{database: database}).DeleteById},
    } {
        t.Run(testCase.name+" delete", func(t *testing.T) {
            if removed, err := testCase.remove(ctx, strings.ToUpper(testCase.id)); nil != err || true == removed { t.Fatalf("alias delete: removed=%v err=%v", removed, err) }
            if removed, err := testCase.remove(ctx, testCase.id); nil != err || false == removed { t.Fatalf("canonical delete: removed=%v err=%v", removed, err) }
        })
    }
}
