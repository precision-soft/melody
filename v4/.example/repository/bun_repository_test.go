package repository

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/.example/entity"
    "github.com/uptrace/bun"
)

type stubResult struct {
    affected    int64
    affectedErr error
}

func (instance stubResult) LastInsertId() (int64, error) {
    return 0, nil
}

func (instance stubResult) RowsAffected() (int64, error) {
    return instance.affected, instance.affectedErr
}

var _ sql.Result = stubResult{}

/* the answer feeds a caller that has to tell a write which landed from one that found no row, so a driver that will not report a count must read as "nothing changed" rather than as a change nobody can confirm */

func TestAffectedAtLeastOneRow(t *testing.T) {
    if true == affectedAtLeastOneRow(nil) {
        t.Fatalf("expected a missing result to report no change")
    }

    if true == affectedAtLeastOneRow(stubResult{affected: 0}) {
        t.Fatalf("expected zero affected rows to report no change")
    }

    if false == affectedAtLeastOneRow(stubResult{affected: 1}) {
        t.Fatalf("expected one affected row to report a change")
    }

    if true == affectedAtLeastOneRow(stubResult{affected: 3, affectedErr: fmt.Errorf("unsupported")}) {
        t.Fatalf("expected a driver that will not report a count to read as no change")
    }
}

/* identicalUpdateDatabase answers every by-id select with the row until the re-read, which finds it only when presentAtReRead holds, the uniqueness count with none, and every update with zero changed rows: what MySQL reports for an update writing the values the row already holds */
func identicalUpdateDatabase(presentAtReRead bool) *bun.DB {
    database, recorder := newFakeBunDatabase()
    recorder.rowsAffected = func(query string) int64 {
        return 0
    }

    reads := 0
    recorder.queryHook = func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "count(*)") {
            return []string{"count"}, [][]driver.Value{{int64(0)}}, nil
        }

        if true == strings.Contains(query, "(id = ") {
            reads = reads + 1
            if 1 == reads || true == presentAtReRead {
                return []string{"id"}, [][]driver.Value{{"row-1"}}, nil
            }
        }

        return []string{}, nil, nil
    }

    return database
}

func identicalUpdateDoors() map[string]func(database *bun.DB) (bool, error) {
    return map[string]func(database *bun.DB) (bool, error){
        "currency": func(database *bun.DB) (bool, error) {
            return newBunCurrencyRepository(database).Update(context.Background(), entity.NewCurrency("cur-1", "EUR", "Euro", 1, time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)))
        },
        "category": func(database *bun.DB) (bool, error) {
            return newBunCategoryRepository(database).Update(context.Background(), entity.NewCategory("cat-1", "Peripherals"))
        },
    }
}

func TestUpdateWritingTheValuesTheRowHoldsIsAnsweredAsFound(t *testing.T) {
    for name, update := range identicalUpdateDoors() {
        t.Run(name, func(t *testing.T) {
            found, updateErr := update(identicalUpdateDatabase(true))
            if nil != updateErr || false == found {
                t.Fatalf("expected an update that changed nothing to find its row, got found=%v err=%v", found, updateErr)
            }
        })
    }
}

func TestUpdateOfARowGoneBeforeTheWriteIsAnsweredAsAbsent(t *testing.T) {
    for name, update := range identicalUpdateDoors() {
        t.Run(name, func(t *testing.T) {
            found, updateErr := update(identicalUpdateDatabase(false))
            if nil != updateErr || true == found {
                t.Fatalf("expected a row gone before the write to be answered as absent, got found=%v err=%v", found, updateErr)
            }
        })
    }
}
