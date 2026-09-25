package repository

import (
    "context"
    "fmt"
    "sort"
    "sync"
    "time"
)

func newInMemoryCatalogReadingRepository() *inMemoryCatalogReadingRepository {
    return &inMemoryCatalogReadingRepository{readingByInstant: map[time.Time]*CatalogReadingRecord{}}
}

/* inMemoryCatalogReadingRepository is the archive of an environment without an archive connection. It keeps the postgres archive's identity rule, one reading per instant, so a test driving the fallback sees the refusals production does. */
type inMemoryCatalogReadingRepository struct {
    mutex            sync.RWMutex
    readingByInstant map[time.Time]*CatalogReadingRecord
}

func (instance *inMemoryCatalogReadingRepository) Append(ctx context.Context, reading *CatalogReadingRecord) error {
    if validateErr := validateCatalogReading(reading); nil != validateErr {
        return validateErr
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    if _, recorded := instance.readingByInstant[reading.TakenAt]; true == recorded {
        return fmt.Errorf("reading already recorded")
    }

    instance.readingByInstant[reading.TakenAt] = copyOfReading(reading)

    return nil
}

/* copyOfReading hands a caller a record without handing it the archive. */
func copyOfReading(reading *CatalogReadingRecord) *CatalogReadingRecord {
    copied := *reading

    return &copied
}

func (instance *inMemoryCatalogReadingRepository) Recent(ctx context.Context, limit int) ([]*CatalogReadingRecord, error) {
    if 0 >= limit {
        return []*CatalogReadingRecord{}, nil
    }

    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    orderedList := make([]*CatalogReadingRecord, 0, len(instance.readingByInstant))
    for _, reading := range instance.readingByInstant {
        orderedList = append(orderedList, copyOfReading(reading))
    }

    /* newest first, the order of the postgres archive's ORDER BY taken_at DESC */
    sort.Slice(orderedList, func(first int, second int) bool {
        return orderedList[first].TakenAt.After(orderedList[second].TakenAt)
    })

    if limit < len(orderedList) {
        orderedList = orderedList[:limit]
    }

    return orderedList, nil
}

func (instance *inMemoryCatalogReadingRepository) Count(ctx context.Context) (int, error) {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return len(instance.readingByInstant), nil
}
