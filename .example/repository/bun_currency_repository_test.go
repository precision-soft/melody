package repository

import (
    "context"
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
    sqlDatabase, err := sql.Open("mysql", "melody:melody@tcp(mysql:3306)/melody_example_v1?parseTime=true")
    if nil != err { t.Fatal(err) }
    sqlDatabase.SetMaxOpenConns(1)
    database := bun.NewDB(sqlDatabase, mysqldialect.New())
    defer database.Close()
    ctx := context.Background()
    for _, kind := range []string{"user", "product", "category", "currency"} {
        table := "melody_example_v1_"+kind
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
