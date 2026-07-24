package repository

import (
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/entity"
)

const concurrentRounds = 500

/* @info every repository is a process-wide singleton and net/http serves each request on its own
goroutine, so a listing request and a deleting request overlap; the writer here always removes a
non-terminal element, which is what makes DeleteById compact the backing array under the reader */

func TestInMemoryProductRepositoryConcurrentReadAndDelete(t *testing.T) {
    repositoryInstance := NewInMemoryProductRepository()

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            for _, product := range repositoryInstance.All() {
                if nil == product {
                    continue
                }

                _ = product.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        now := time.Now()

        for round := 0; round < concurrentRounds; round++ {
            first := entity.NewProduct("prod-first", "first", "black", "cat-1", 1, "cur-eur", 1, now, now)
            second := entity.NewProduct("prod-second", "second", "black", "cat-1", 1, "cur-eur", 1, now, now)

            createErr := repositoryInstance.Create(first)
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(second)
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById("prod-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById("prod-second")
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    if 5 != len(repositoryInstance.All()) {
        t.Fatalf("expected the seeded products to survive, got %d", len(repositoryInstance.All()))
    }
}

func TestInMemoryCategoryRepositoryConcurrentReadAndDelete(t *testing.T) {
    repositoryInstance := NewInMemoryCategoryRepository()

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            for _, category := range repositoryInstance.All() {
                if nil == category {
                    continue
                }

                _ = category.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            createErr := repositoryInstance.Create(entity.NewCategory("cat-first", "first"))
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(entity.NewCategory("cat-second", "second"))
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById("cat-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById("cat-second")
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    if 4 != len(repositoryInstance.All()) {
        t.Fatalf("expected the seeded categories to survive, got %d", len(repositoryInstance.All()))
    }
}

func TestInMemoryCurrencyRepositoryConcurrentReadAndDelete(t *testing.T) {
    repositoryInstance := NewInMemoryCurrencyRepository()

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            for _, currency := range repositoryInstance.All() {
                if nil == currency {
                    continue
                }

                _ = currency.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            createErr := repositoryInstance.Create(entity.NewCurrency("cur-first", "AAA", "first"))
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(entity.NewCurrency("cur-second", "BBB", "second"))
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById("cur-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById("cur-second")
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    if 3 != len(repositoryInstance.All()) {
        t.Fatalf("expected the seeded currencies to survive, got %d", len(repositoryInstance.All()))
    }
}

func TestInMemoryUserRepositoryConcurrentReadAndDelete(t *testing.T) {
    repositoryInstance := NewInMemoryUserRepository()

    waitGroup := sync.WaitGroup{}
    waitGroup.Add(2)

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            users, allErr := repositoryInstance.All()
            if nil != allErr {
                t.Errorf("all: %v", allErr)
                return
            }

            for _, user := range users {
                if nil == user {
                    continue
                }

                _ = user.Id
            }
        }
    }()

    go func() {
        defer waitGroup.Done()

        for round := 0; round < concurrentRounds; round++ {
            createErr := repositoryInstance.Create(entity.NewUser("user-first", "first", "hash", []string{entity.RoleUser}))
            if nil != createErr {
                t.Errorf("create first: %v", createErr)
                return
            }

            createErr = repositoryInstance.Create(entity.NewUser("user-second", "second", "hash", []string{entity.RoleUser}))
            if nil != createErr {
                t.Errorf("create second: %v", createErr)
                return
            }

            _, deleteErr := repositoryInstance.DeleteById("user-first")
            if nil != deleteErr {
                t.Errorf("delete first: %v", deleteErr)
                return
            }

            _, deleteErr = repositoryInstance.DeleteById("user-second")
            if nil != deleteErr {
                t.Errorf("delete second: %v", deleteErr)
                return
            }
        }
    }()

    waitGroup.Wait()

    users, allErr := repositoryInstance.All()
    if nil != allErr {
        t.Fatalf("all: %v", allErr)
    }

    if 3 != len(users) {
        t.Fatalf("expected the seeded users to survive, got %d", len(users))
    }
}

/* @info All() must hand back a copy: mutating the returned slice may not reach the repository */

func TestInMemoryProductRepositoryAllReturnsCopy(t *testing.T) {
    repositoryInstance := NewInMemoryProductRepository()

    products := repositoryInstance.All()
    if 0 == len(products) {
        t.Fatalf("expected seeded products")
    }

    products[0] = nil

    reread := repositoryInstance.All()
    if nil == reread[0] {
        t.Fatalf("expected the repository slice to be unaffected by a caller write")
    }
}
