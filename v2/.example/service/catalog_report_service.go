package service

import (
    "context"
    "strconv"
    "time"

    "github.com/precision-soft/melody/v2/.example/entity"
    melodyclockcontract "github.com/precision-soft/melody/v2/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
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
    clock             melodyclockcontract.Clock
    backend           CatalogReportBackend
    productRepository catalogSizeSource
    journalRepository catalogJournalSource
}

/* catalogSizeSource is what the report needs from the product repository: how large the nomenclature currently is. */
type catalogSizeSource interface {
    All(ctx context.Context) ([]*entity.Product, error)
}

/* catalogJournalSource is what the report needs from the journal: how many changes it has recorded. The journal creates its own table when it is built, so the report can ask for the count without first making sure there is somewhere to count. */
type catalogJournalSource interface {
    Count(ctx context.Context) (int, error)
}

func NewCatalogReportService(
    clockInstance melodyclockcontract.Clock,
    backend CatalogReportBackend,
    productRepository catalogSizeSource,
    journalRepository catalogJournalSource,
) *CatalogReportService {
    return &CatalogReportService{
        clock:             clockInstance,
        backend:           backend,
        productRepository: productRepository,
        journalRepository: journalRepository,
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

    return instance.recompute(ctx, recordedAt)
}

/* Refresh takes a new reading and leaves it in the cache whatever was there before, which is what the scheduled command calls: a report nobody asked for in the last window is the one that would otherwise be computed inside a request. */
func (instance *CatalogReportService) Refresh(ctx context.Context) (*CatalogReport, error) {
    return instance.recompute(ctx, instance.clock.Now())
}

func (instance *CatalogReportService) recompute(ctx context.Context, recordedAt time.Time) (*CatalogReport, error) {
    productCount := 0
    if nil != instance.productRepository {
        products, allErr := instance.productRepository.All(ctx)
        if nil != allErr {
            return nil, allErr
        }

        productCount = len(products)
    }

    /* without a database there is no journal, and the report says so with a count of zero rather than by being absent: the reading is about the catalogue, and the catalogue is there either way */
    journalCount := 0
    if nil != instance.journalRepository {
        count, countErr := instance.journalRepository.Count(ctx)
        if nil != countErr {
            return nil, countErr
        }

        journalCount = count
    }

    payload := "products=" + strconv.Itoa(productCount) +
        " journal=" + strconv.Itoa(journalCount) +
        " recorded_at=" + recordedAt.UTC().Format(time.RFC3339)

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
