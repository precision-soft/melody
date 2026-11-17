package encrypt

import (
    "context"
    "database/sql/driver"
    "strings"
    "testing"
    "time"
)

func TestMigrate_AStalePinnedConnectionIsTradedOnceBeforeTheJudge(t *testing.T) {
    migrator, stub := newScriptedMigrator(t, judgeTable(accountsColumns, accountsPrimary, nil))
    stub.failFirstPing = true

    if _, runErr := migrator.MigrateEncrypt(context.Background(), TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}}); nil != runErr {
        t.Fatalf("expected the run on a fresh connection, got %v", runErr)
    }

    stub.mutex.Lock()
    defer stub.mutex.Unlock()
    if 2 != stub.opens || 1 != stub.pings {
        t.Fatalf("expected one failed ping and one fresh connection, got opens %d pings %d", stub.opens, stub.pings)
    }
}

func TestMigrate_ARefusedJudgeHandsItsConnectionBack(t *testing.T) {
    migrator, _ := newScriptedMigrator(t, judgeTable(accountsColumns, [][]driver.Value{}, nil))
    migrator.db.DB.SetMaxOpenConns(1)

    for attempt := 0; attempt < 2; attempt++ {
        bounded, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        _, runErr := migrator.MigrateEncrypt(bounded, TableSpec{Table: "accounts", PrimaryKey: "id", Columns: []string{"notes"}})
        cancel()

        if nil == runErr || false == strings.Contains(runErr.Error(), "the paging key is not unique") {
            t.Fatalf("attempt %d: expected the judge's refusal, the connection handed back by the one before, got %v", attempt, runErr)
        }
    }
}
