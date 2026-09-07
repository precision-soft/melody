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

/* inMemoryCatalogReadingRepository is what an environment without an archive connection gets. It keeps the same identity rule its postgres sister keeps — one reading per instant — because a fallback that accepted what the real one refuses would let a defect reach production through the only path a test can drive. */
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

/* copyOfReading is why the archive can hand a caller a record without handing it the archive. It is a
   function rather than two lines at each of the two sites because a copy written inline cannot be taken
   away by anything a test can observe: removing it leaves the local unused and the package stops
   compiling, so the guard would have had no mutant and no proof. Here it has both. */
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

    /* newest first, the order the postgres sister's ORDER BY taken_at DESC produces: a caller reading through either implementation sees the same archive. */
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
