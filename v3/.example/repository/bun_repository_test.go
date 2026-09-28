package repository

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "fmt"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/entity"
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

func identifierMintLockAnswering(acquired int64) func(query string) ([]string, [][]driver.Value, error) {
    return func(query string) ([]string, [][]driver.Value, error) {
        if true == strings.Contains(query, "GET_LOCK") {
            return []string{"acquired"}, [][]driver.Value{{acquired}}, nil
        }

        return []string{}, nil, nil
    }
}

func indexOfFirstQuery(queries []string, matcher func(query string) bool) int {
    for index, query := range queries {
        if true == matcher(query) {
            return index
        }
    }

    return -1
}

/* two creates that read the identifier list before either inserted mint the same identifier; the lock is taken before the read and released after the insert, so the read of the next create sees the row of the one before */
func TestInsertWithMintedIdentifier_ReadsAndInsertsUnderTheTablesAdvisoryLock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = identifierMintLockAnswering(1)

    repository := &bunCategoryRepository{database: database}
    category := entity.NewCategory("", "Probe")

    if createErr := repository.Create(context.Background(), category); nil != createErr {
        t.Fatalf("unexpected create error: %v", createErr)
    }

    if "cat-1" != category.Id {
        t.Fatalf("expected the minted id cat-1, got %q", category.Id)
    }

    queries := recorder.recordedQueries()
    lockIndex := indexOfFirstQuery(queries, func(query string) bool {
        return true == strings.Contains(query, "GET_LOCK")
    })
    readIndex := indexOfFirstQuery(queries, func(query string) bool {
        return true == strings.HasPrefix(query, "SELECT") && false == strings.Contains(query, "GET_LOCK")
    })
    insertIndex := indexOfFirstQuery(queries, func(query string) bool {
        return true == strings.HasPrefix(query, "INSERT")
    })
    releaseIndex := indexOfFirstQuery(queries, func(query string) bool {
        return true == strings.Contains(query, "RELEASE_LOCK")
    })

    if false == (0 <= lockIndex && lockIndex < readIndex && readIndex < insertIndex && insertIndex < releaseIndex) {
        t.Fatalf("expected lock, read, insert, release in that order, got %v", queries)
    }
}

func TestInsertWithMintedIdentifier_InsertsASuppliedIdentifierWithoutTheLock(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = identifierMintLockAnswering(1)

    repository := &bunCategoryRepository{database: database}

    if createErr := repository.Create(context.Background(), entity.NewCategory("cat-supplied", "Probe")); nil != createErr {
        t.Fatalf("unexpected create error: %v", createErr)
    }

    if lockCount := recorder.countMatching(func(query string) bool {
        return true == strings.Contains(query, "GET_LOCK")
    }); 0 != lockCount {
        t.Fatalf("expected no lock around a supplied identifier, got %v", recorder.recordedQueries())
    }
}

/* GET_LOCK answers 0 when its wait runs out; the create is refused rather than minting outside the lock */
func TestInsertWithMintedIdentifier_RefusesWhenTheLockIsNotTaken(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = identifierMintLockAnswering(0)

    repository := &bunCategoryRepository{database: database}

    createErr := repository.Create(context.Background(), entity.NewCategory("", "Probe"))
    if nil == createErr || false == strings.Contains(createErr.Error(), "was not taken") {
        t.Fatalf("expected the refusal naming the lock, got %v", createErr)
    }

    if insertCount := recorder.countMatching(func(query string) bool {
        return true == strings.HasPrefix(query, "INSERT")
    }); 0 != insertCount {
        t.Fatalf("expected no insert without the lock, got %v", recorder.recordedQueries())
    }
}

/* a create waiting behind another create of this process holds no connection: it waits at the gate, where its context still ends it, and reaches GET_LOCK only once the gate is free */
func TestInsertWithMintedIdentifier_WaitsAtTheProcessGateWithoutAConnection(t *testing.T) {
    database, recorder := newFakeBunDatabase()
    recorder.queryHook = identifierMintLockAnswering(1)

    gate := identifierMintGate(categoryIdentifierMintLockName)
    gate <- struct{}{}
    defer func() {
        <-gate
    }()

    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()

    createErr := (&bunCategoryRepository{database: database}).Create(ctx, entity.NewCategory("", "Probe"))
    if false == errors.Is(createErr, context.DeadlineExceeded) {
        t.Fatalf("expected the wait at the gate to end with the context, got %v", createErr)
    }

    if lockCount := recorder.countMatching(func(query string) bool {
        return true == strings.Contains(query, "GET_LOCK")
    }); 0 != lockCount {
        t.Fatalf("expected no GET_LOCK while the gate is held, got %v", recorder.recordedQueries())
    }
}

/* the read before the insert cannot stop a concurrent create of the same supplied identifier, so the primary key's refusal of it is answered as the read's would be */
func TestInsertWithMintedIdentifier_AnswersThePrimaryKeysRefusalOfASuppliedIdentifierAsTaken(t *testing.T) {
    refusal := fmt.Errorf("Error 1062 (23000): Duplicate entry 'prod-supplied' for key 'melody_example_v3_product.PRIMARY'")

    insertErr := insertWithMintedIdentifier(
        context.Background(),
        nil,
        productIdentifierMintLockName,
        false,
        func() error {
            t.Fatalf("expected no mint for a supplied identifier")

            return nil
        },
        func() error {
            return fmt.Errorf("insert failed: %w", refusal)
        },
    )

    if false == errors.Is(insertErr, ErrIdAlreadyExists) {
        t.Fatalf("expected the taken identifier's refusal, got %v", insertErr)
    }
}

func TestAsIdAlreadyExists_LeavesEveryOtherFailureAlone(t *testing.T) {
    if nil != asIdAlreadyExists(nil) {
        t.Fatalf("expected no refusal for a write that landed")
    }

    for _, failure := range []error{
        fmt.Errorf("Error 1062 (23000): Duplicate entry 'probe' for key 'melody_example_v3_user.user_username_unique'"),
        errors.New("connection refused"),
    } {
        if failure != asIdAlreadyExists(failure) {
            t.Fatalf("expected %q answered untouched, got %v", failure, asIdAlreadyExists(failure))
        }
    }
}
