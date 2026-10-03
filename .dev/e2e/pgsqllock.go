package main

import (
    "time"

    pgsql "github.com/precision-soft/melody/integrations/bunorm/pgsql/v3"
)

/* runPgsqlLockCheck exercises the PostgreSQL session-advisory-lock Locker against a live postgres: two independent locks on the same name contend (each pins its own backend session), and releasing the holder hands the lock to the waiter — the mutual-exclusion primitive the outbox relay and cron leader-election rely on. */
func runPgsqlLockCheck(dsn string) {
    database := openPostgres(dsn)
    defer database.Close()

    locker := pgsql.NewLocker(database)

    holder := locker.CreateLock("melody:e2e:advisory", 30*time.Second)
    contender := locker.CreateLock("melody:e2e:advisory", 30*time.Second)

    acquiredHolder, holderErr := holder.Acquire(newRuntime())
    if nil != holderErr {
        fail("holder acquire: %v", holderErr)
    }
    if false == acquiredHolder {
        fail("expected the holder to acquire a free advisory lock")
    }
    pass("holder acquired the advisory lock")

    /* a second, independent lock on the same name must NOT be able to acquire it while the holder's backend session still holds it */
    acquiredContender, contenderErr := contender.Acquire(newRuntime())
    if nil != contenderErr {
        fail("contender acquire (while held): %v", contenderErr)
    }
    if true == acquiredContender {
        fail("expected the contender to be denied while the lock is held")
    }
    pass("contender denied while the lock is held (mutual exclusion)")

    /* release the holder; the contender can now take it */
    if releaseErr := holder.Release(newRuntime()); nil != releaseErr {
        fail("holder release: %v", releaseErr)
    }

    acquiredAfterRelease, afterErr := contender.Acquire(newRuntime())
    if nil != afterErr {
        fail("contender acquire (after release): %v", afterErr)
    }
    if false == acquiredAfterRelease {
        fail("expected the contender to acquire the lock after the holder released it")
    }
    pass("holder released → contender acquired (hand-off)")

    if releaseErr := contender.Release(newRuntime()); nil != releaseErr {
        fail("contender release: %v", releaseErr)
    }
    pass("contender released the lock")
}
