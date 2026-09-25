package repository

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "time"
)

/* inMemoryJournalCapacity bounds the process-local journal, since nothing removes an entry. */
const inMemoryJournalCapacity = 200

func newInMemoryCatalogJournalRepository() CatalogJournalRepository {
    return &inMemoryCatalogJournalRepository{}
}

/* inMemoryCatalogJournalRepository keeps the journal inside the process, for an application booted without a database. */
type inMemoryCatalogJournalRepository struct {
    mutex      sync.RWMutex
    entries    []*CatalogJournalEntry
    nextId     int64
    totalCount int
}

func (instance *inMemoryCatalogJournalRepository) Append(ctx context.Context, entry *CatalogJournalEntry) (*CatalogJournalEntry, error) {
    validationErr := validateCatalogJournalEntry(entry)
    if nil != validationErr {
        return nil, validationErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.appendLocked(entry), nil
}

/* AppendBatch stores the whole batch under one lock, so a request's entries are not interleaved with another's; every entry is validated before anything is stored. */
func (instance *inMemoryCatalogJournalRepository) AppendBatch(ctx context.Context, entryList []*CatalogJournalEntry) error {
    if 0 == len(entryList) {
        return nil
    }

    for _, entry := range entryList {
        validationErr := validateCatalogJournalEntry(entry)
        if nil != validationErr {
            return validationErr
        }
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, entry := range entryList {
        instance.appendLocked(entry)
    }

    return nil
}

/* appendLocked stores one validated entry. The caller holds the lock. */
func (instance *inMemoryCatalogJournalRepository) appendLocked(entry *CatalogJournalEntry) *CatalogJournalEntry {
    instance.nextId = instance.nextId + 1

    stored := &CatalogJournalEntry{
        Id:         instance.nextId,
        RequestId:  entry.RequestId,
        Actor:      entry.Actor,
        Action:     entry.Action,
        Subject:    entry.Subject,
        SubjectId:  entry.SubjectId,
        RecordedAt: entry.RecordedAt,
    }

    if "" == strings.TrimSpace(stored.Actor) {
        stored.Actor = CatalogJournalActorSystem
    }

    if true == stored.RecordedAt.IsZero() {
        stored.RecordedAt = time.Now()
    }

    instance.entries = append(instance.entries, stored)
    instance.totalCount = instance.totalCount + 1

    /* the oldest entries are dropped rather than the newest refused, so a full journal never changes what the application does */
    if inMemoryJournalCapacity < len(instance.entries) {
        instance.entries = instance.entries[len(instance.entries)-inMemoryJournalCapacity:]
    }

    return stored
}

func validateCatalogJournalEntry(entry *CatalogJournalEntry) error {
    if nil == entry {
        return fmt.Errorf("journal entry is required")
    }

    if "" == strings.TrimSpace(entry.Action) {
        return fmt.Errorf("action is required")
    }

    if "" == strings.TrimSpace(entry.Subject) {
        return fmt.Errorf("subject is required")
    }

    return nil
}

func (instance *inMemoryCatalogJournalRepository) Latest(ctx context.Context, limit int) ([]*CatalogJournalEntry, error) {
    if 0 >= limit {
        limit = 10
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    entries := make([]*CatalogJournalEntry, 0, limit)

    for index := len(instance.entries) - 1; 0 <= index; index = index - 1 {
        if limit <= len(entries) {
            break
        }

        entries = append(entries, instance.entries[index])
    }

    return entries, nil
}

/* Count reports every entry the process has recorded, including the ones the bound has dropped. */
func (instance *inMemoryCatalogJournalRepository) Count(ctx context.Context) (int, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.totalCount, nil
}

var _ CatalogJournalRepository = (*inMemoryCatalogJournalRepository)(nil)
