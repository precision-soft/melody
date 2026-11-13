package migrate

import (
    "context"
    "database/sql"
    "testing"

    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect"
    "github.com/uptrace/bun/schema"
)

func TestFetchDatabaseIdentity_AnswersNilForADialectWithoutAFetcher(t *testing.T) {
    database, _ := newFakeBunDatabase()

    identity, identityErr := fetchDatabaseIdentity(context.Background(), database)
    if nil != identityErr {
        t.Fatalf("unexpected error: %v", identityErr)
    }

    if nil != identity {
        t.Fatalf("expected no identity for the fake dialect, got %+v", identity)
    }
}

/* dialectNamedAs plays any dialect name over the fake driver, so the dispatch of fetchDatabaseIdentity can be observed per dialect without a live server: the fetchers themselves need one, the dispatch does not. */
type dialectNamedAs struct {
    schema.Dialect

    name dialect.Name
}

func (instance dialectNamedAs) Name() dialect.Name {
    return instance.name
}

func TestFetchDatabaseIdentity_DispatchesPerDialect(t *testing.T) {
    testCases := []struct {
        name         string
        dialectName  dialect.Name
        reachesFetch bool
    }{
        {name: "mysql has a fetcher", dialectName: dialect.MySQL, reachesFetch: true},
        {name: "pgsql has a fetcher", dialectName: dialect.PG, reachesFetch: true},
        {name: "any other dialect answers nil", dialectName: dialect.SQLite, reachesFetch: false},
    }

    for _, testCase := range testCases {
        t.Run(testCase.name, func(t *testing.T) {
            sqlDatabase := sql.OpenDB(&fakeConnector{recorder: &queryRecorder{}})
            database := bun.NewDB(sqlDatabase, dialectNamedAs{Dialect: newFakeDialect(), name: testCase.dialectName})

            identity, identityErr := fetchDatabaseIdentity(context.Background(), database)

            if false == testCase.reachesFetch {
                if nil != identityErr || nil != identity {
                    t.Fatalf("expected a nil identity with no error, got %+v / %v", identity, identityErr)
                }

                return
            }

            /* the fake driver answers no rows for the identity columns, so reaching the fetcher shows as an error rather than an identity — which is the signal: the dialect was dispatched instead of skipped */
            if nil == identityErr && nil == identity {
                t.Fatalf("expected the dialect to reach its fetcher")
            }
        })
    }
}
