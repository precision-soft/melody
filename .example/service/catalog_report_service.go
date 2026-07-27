package service

import (
    "context"
    "strconv"
    "time"

    melodyclockcontract "github.com/precision-soft/melody/clock/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
)

const (
    ServiceCatalogReportService = "service.example.catalog.report.service"

    catalogReportCacheKey = "catalog.report"
    catalogReportCacheTtl = time.Minute
)

/* CatalogReportBackend is the slice of a cache backend this service uses. Declaring it here rather than taking the integration's type keeps the service testable without a redis, and keeps the example honest about what it actually needs. */
type CatalogReportBackend interface {
    Get(key string) ([]byte, bool, error)

    Set(key string, payload []byte, ttl time.Duration) error
}

/* CatalogReport is one reading of the catalogue, stamped by the clock the service was built with. */
type CatalogReport struct {
    RecordedAt time.Time
    Payload    string
    FromCache  bool
}

/* CatalogReportService stamps a reading of the catalogue and remembers it for a while.

The clock is injected rather than read from the wall, which is what makes the stamp assertable: a frozen clock lets a test state the exact instant a report carries and the exact instant a cached one still carries, neither of which can be written against time.Now. */
type CatalogReportService struct {
    clock          melodyclockcontract.Clock
    backend        CatalogReportBackend
    noteRepository catalogNoteSource
}

/* catalogNoteSource is what the report needs from the note repository. It carries EnsureSchema beside Count because the report must not depend on another route having run first: the demo table is created by whichever path reaches it, and a report that assumed the table already existed failed with a missing-table error on a fresh database. */
type catalogNoteSource interface {
    EnsureSchema(ctx context.Context) error

    Count(ctx context.Context) (int, error)
}

func NewCatalogReportService(
    clockInstance melodyclockcontract.Clock,
    backend CatalogReportBackend,
    noteRepository catalogNoteSource,
) *CatalogReportService {
    return &CatalogReportService{
        clock:          clockInstance,
        backend:        backend,
        noteRepository: noteRepository,
    }
}

/* Report yields the current reading, from the cache when one is still warm. A cached reading keeps the instant it was taken at, which is the whole point of stamping it: the caller can tell how old the answer is. */
func (instance *CatalogReportService) Report(ctx context.Context) (*CatalogReport, error) {
    recordedAt := instance.clock.Now()

    if nil != instance.backend {
        payload, found, getErr := instance.backend.Get(catalogReportCacheKey)
        if nil != getErr {
            return nil, getErr
        }

        if true == found {
            return &CatalogReport{
                RecordedAt: recordedAt,
                Payload:    string(payload),
                FromCache:  true,
            }, nil
        }
    }

    noteCount := 0
    if nil != instance.noteRepository {
        ensureSchemaErr := instance.noteRepository.EnsureSchema(ctx)
        if nil != ensureSchemaErr {
            return nil, ensureSchemaErr
        }

        count, countErr := instance.noteRepository.Count(ctx)
        if nil != countErr {
            return nil, countErr
        }

        noteCount = count
    }

    payload := "notes=" + strconv.Itoa(noteCount) + " recorded_at=" + recordedAt.UTC().Format(time.RFC3339)

    if nil != instance.backend {
        setErr := instance.backend.Set(catalogReportCacheKey, []byte(payload), catalogReportCacheTtl)
        if nil != setErr {
            return nil, setErr
        }
    }

    return &CatalogReport{
        RecordedAt: recordedAt,
        Payload:    payload,
        FromCache:  false,
    }, nil
}

func MustGetCatalogReportService(resolver melodycontainercontract.Resolver) *CatalogReportService {
    return melodycontainer.MustFromResolver[*CatalogReportService](resolver, ServiceCatalogReportService)
}
