package cli

import (
    "bytes"
    "context"
    "errors"
    "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/reporting"
    "github.com/precision-soft/melody/v3/.example/repository"
    "github.com/precision-soft/melody/v3/.example/service"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodycachecontract "github.com/precision-soft/melody/v3/cache/contract"
    melodyclock "github.com/precision-soft/melody/v3/clock"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/httpclient"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

/* refreshSequence is the one ledger every collaborator of the refresh writes into — the lock, the cache the reading lands in, the archive, the sink — so the ORDER the command drove them in is a list the test can read, not a set of counters that agree with any order. */
type refreshSequence struct {
    mutex  sync.Mutex
    events []string
}

func (instance *refreshSequence) record(event string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.events = append(instance.events, event)
}

func (instance *refreshSequence) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.events...)
}

func (instance *refreshSequence) count(event string) int {
    count := 0
    for _, recorded := range instance.recorded() {
        if event == recorded {
            count++
        }
    }

    return count
}

func (instance *refreshSequence) indexOf(event string) int {
    for index, recorded := range instance.recorded() {
        if event == recorded {
            return index
        }
    }

    return -1
}

/* sequenceLocker hands out a lock that records its turns and can be told to answer that the lock is held elsewhere */
type sequenceLocker struct {
    sequence *refreshSequence
    inner    melodylockcontract.Locker
    heldAway bool
}

func (instance *sequenceLocker) CreateLock(name string, ttl time.Duration) melodylockcontract.Lock {
    return &sequenceLock{sequence: instance.sequence, inner: instance.inner.CreateLock(name, ttl), heldAway: instance.heldAway}
}

type sequenceLock struct {
    sequence *refreshSequence
    inner    melodylockcontract.Lock
    heldAway bool
}

func (instance *sequenceLock) Acquire(runtimeInstance melodyruntimecontract.Runtime) (bool, error) {
    if true == instance.heldAway {
        instance.sequence.record("acquire-refused")

        return false, nil
    }

    instance.sequence.record("acquire")

    return instance.inner.Acquire(runtimeInstance)
}

func (instance *sequenceLock) Release(runtimeInstance melodyruntimecontract.Runtime) error {
    instance.sequence.record("release")

    return instance.inner.Release(runtimeInstance)
}

func (instance *sequenceLock) Refresh(runtimeInstance melodyruntimecontract.Runtime, ttl time.Duration) error {
    return instance.inner.Refresh(runtimeInstance, ttl)
}

/* sequenceCache records the moment the reading lands in the cache, which is the moment Refresh took it: the product list the reading counts is cached through the same door, so the key is what tells the reading's own write apart */
type sequenceCache struct {
    melodycachecontract.Cache

    sequence *refreshSequence
}

const readingCacheKeyUnderTest = "catalog.reading"

func (instance *sequenceCache) Set(key string, value any, ttl time.Duration) error {
    if readingCacheKeyUnderTest == key {
        instance.sequence.record("refresh")
    }

    return instance.Cache.Set(key, value, ttl)
}

/* sequenceArchive is the archive as the command reaches it: one row per instant, refused as the real ones refuse it, every append on the ledger */
type sequenceArchive struct {
    sequence *refreshSequence
    mutex    sync.Mutex
    rows     []*repository.CatalogReadingRecord
}

func (instance *sequenceArchive) Append(ctx context.Context, record *repository.CatalogReadingRecord) error {
    instance.sequence.record("archive")

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    for _, existing := range instance.rows {
        if true == existing.TakenAt.Equal(record.TakenAt) {
            return errors.New("reading already recorded")
        }
    }

    instance.rows = append(instance.rows, record)

    return nil
}

func (instance *sequenceArchive) Recent(ctx context.Context, limit int) ([]*repository.CatalogReadingRecord, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]*repository.CatalogReadingRecord{}, instance.rows...), nil
}

func (instance *sequenceArchive) Count(ctx context.Context) (int, error) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.rows), nil
}

func (instance *sequenceArchive) appended() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.rows)
}

var _ repository.CatalogReadingRepository = (*sequenceArchive)(nil)

type refreshFixture struct {
    sequence *refreshSequence
    archive  *sequenceArchive
    runtime  melodyruntimecontract.Runtime
    sink     *httptest.Server
    sinkHits int
}

type refreshFixtureOption struct {
    lockerProvider func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error)
    withoutLocker  bool
    sinkStatus     int
}

/* newRefreshFixture is the composition root of the refresh, on doubles that write into one ledger: the report service and the exporter are registered by TYPE, as the generated wiring registers them, and the archive, the locker and the export client by NAME, as the composition root registers them */
func newRefreshFixture(t *testing.T, option refreshFixtureOption) *refreshFixture {
    t.Helper()

    sequence := &refreshSequence{}
    archive := &sequenceArchive{sequence: sequence}
    clockInstance := melodyclock.NewFrozenClock(time.Date(2026, time.September, 13, 9, 0, 0, 0, time.UTC))

    backend := melodycache.NewInMemoryBackend(0, 0, clockInstance)
    cacheInstance := &sequenceCache{Cache: melodycache.NewManagerOwningBackend(backend, melodycache.NewJsonSerializer()), sequence: sequence}

    storage := persistence.NewCatalogStorage(nil)
    productRepository, productErr := repository.NewProductRepository(storage)
    if nil != productErr {
        t.Fatalf("build the product repository: %v", productErr)
    }
    journalRepository, journalErr := repository.NewCatalogJournalRepository(storage)
    if nil != journalErr {
        t.Fatalf("build the journal repository: %v", journalErr)
    }

    reportService, serviceErr := reporting.NewCatalogReportService(
        reporting.NewReportFormatter(),
        service.NewProductService(productRepository, nil, nil, cacheInstance, nil, clockInstance),
        journalRepository,
        cacheInstance,
        clockInstance,
        "catalog",
        10,
        time.Minute,
    )
    if nil != serviceErr {
        t.Fatalf("build the report service: %v", serviceErr)
    }

    fixture := &refreshFixture{sequence: sequence, archive: archive}

    sinkStatus := option.sinkStatus
    if 0 == sinkStatus {
        sinkStatus = http.StatusOK
    }
    fixture.sink = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        sequence.record("export")
        fixture.sinkHits++
        writer.WriteHeader(sinkStatus)
    }))
    t.Cleanup(fixture.sink.Close)

    serviceContainer := melodycontainer.NewContainer()

    melodycontainer.MustRegisterType(serviceContainer, func(resolver melodycontainercontract.Resolver) (*reporting.CatalogReportService, error) {
        return reportService, nil
    })
    melodycontainer.MustRegisterType(serviceContainer, func(resolver melodycontainercontract.Resolver) (*reporting.CatalogReportExporter, error) {
        return reporting.NewCatalogReportExporter(fixture.sink.URL + "/v1/report-sink"), nil
    })
    melodycontainer.MustRegister(
        serviceContainer,
        service.ServiceReportExportHttpClient,
        func(resolver melodycontainercontract.Resolver) (*httpclient.HttpClient, error) {
            return httpclient.NewHttpClient(httpclient.NewHttpClientConfig("", 2*time.Second, nil).WithoutRedirects()), nil
        },
        melodycontainer.WithoutTypeRegistration(),
    )
    melodycontainer.MustRegister(
        serviceContainer,
        repository.ServiceCatalogReadingRepository,
        func(resolver melodycontainercontract.Resolver) (repository.CatalogReadingRepository, error) {
            return archive, nil
        },
    )

    if false == option.withoutLocker {
        lockerProvider := option.lockerProvider
        if nil == lockerProvider {
            lockerProvider = func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
                return &sequenceLocker{sequence: sequence, inner: melodylock.NewInMemoryLocker(clockInstance)}, nil
            }
        }

        melodycontainer.MustRegister(serviceContainer, persistence.ServiceArchiveLocker, lockerProvider, melodycontainer.WithoutTypeRegistration())
    }

    t.Cleanup(func() { _ = serviceContainer.Close() })

    fixture.runtime = melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    return fixture
}

func runRefresh(t *testing.T, fixture *refreshFixture) (string, error) {
    t.Helper()

    buffer := &bytes.Buffer{}
    runErr := NewCatalogReportRefreshCommand().Run(fixture.runtime, newBoolFlagContext("none", false, buffer))

    return buffer.String(), runErr
}

/* the run is one unit under the lock, taken first and released last, with the archive written before the sink is told: that is the order the ledger has to read, and the one every mutant of the command changes */
func TestCatalogReportRefreshCommandDrivesLockRefreshArchiveExportInThatOrder(t *testing.T) {
    fixture := newRefreshFixture(t, refreshFixtureOption{})

    output, runErr := runRefresh(t, fixture)
    if nil != runErr {
        t.Fatalf("expected the refresh to succeed, got %v", runErr)
    }

    wanted := []string{"acquire", "refresh", "archive", "export", "release"}
    if strings.Join(wanted, ",") != strings.Join(fixture.sequence.recorded(), ",") {
        t.Fatalf("expected the run to drive %v, it drove %v", wanted, fixture.sequence.recorded())
    }

    if false == strings.Contains(output, "true") || false == strings.Contains(output, "ARCHIVED") {
        t.Fatalf("expected the table to report the export and the archive, got %q", output)
    }
}

/* a lock held elsewhere means another process is taking this reading right now: this one takes none, exports none, records none, and says so with a zero exit */
func TestCatalogReportRefreshCommandSkipsTheWholeRunWhenTheLockIsHeldElsewhere(t *testing.T) {
    sequenceHolder := &refreshSequence{}
    fixture := newRefreshFixture(t, refreshFixtureOption{
        lockerProvider: func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            return &sequenceLocker{sequence: sequenceHolder, inner: melodylock.NewInMemoryLocker(melodyclock.NewSystemClock()), heldAway: true}, nil
        },
    })

    output, runErr := runRefresh(t, fixture)
    if nil != runErr {
        t.Fatalf("expected a lost lock to be a zero exit, got %v", runErr)
    }

    if 0 != fixture.sequence.count("refresh") || 0 != fixture.sinkHits || 0 != fixture.archive.appended() {
        t.Fatalf("expected no reading, no export and no row behind a lost lock, got refresh=%d export=%d archive=%d", fixture.sequence.count("refresh"), fixture.sinkHits, fixture.archive.appended())
    }

    if false == strings.Contains(output, "another process is taking this reading") {
        t.Fatalf("expected the command to say another process is taking the reading, got %q", output)
    }
}

/* a locker that is registered and cannot be resolved is the archive being unreachable — its provider opens the postgres handle — and that is a failure handed back, where it used to read as "no archive wired" and exit zero over a reading never recorded. It is handed back AFTER the reading and the export, which need nothing from postgres: the archive's outage does not take the two halves that do not depend on it. */
func TestCatalogReportRefreshCommandHandsBackAnUnreachableArchiveAfterReadingAndExporting(t *testing.T) {
    refusal := errors.New("dial tcp 172.18.0.10:5432: connect: connection refused")
    fixture := newRefreshFixture(t, refreshFixtureOption{
        lockerProvider: func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            /* what the composition root's provider hands back: the archive database named with where it is, the driver's refusal as the cause */
            return nil, exception.NewError("the archive database at postgres:5432/melody_example_v3 could not be opened", nil, refusal)
        },
    })

    output, runErr := runRefresh(t, fixture)
    if nil == runErr {
        t.Fatalf("expected the unreachable archive to fail the run")
    }

    if false == strings.Contains(runErr.Error(), "the archive database at postgres:5432/melody_example_v3 could not be opened") {
        t.Fatalf("expected the failure to name the archive and where it is, got %v", runErr)
    }

    if false == errors.Is(runErr, refusal) {
        t.Fatalf("expected the driver's refusal to stay the cause, got %v", runErr)
    }

    if 1 != fixture.sequence.count("refresh") || 1 != fixture.sinkHits || 0 != fixture.archive.appended() {
        t.Fatalf("expected the reading taken and exported and nothing archived, got refresh=%d export=%d archive=%d", fixture.sequence.count("refresh"), fixture.sinkHits, fixture.archive.appended())
    }

    if false == strings.Contains(output, "the reading was taken and exported; the archive did not record it") {
        t.Fatalf("expected the operator to be told what happened, got %q", output)
    }
}

/* with no locker registered there is no archive: the run reads and exports as it did before there was one, and reports the archive as not written rather than failing over it */
func TestCatalogReportRefreshCommandRunsWithoutAnArchiveWhenNoneIsWired(t *testing.T) {
    fixture := newRefreshFixture(t, refreshFixtureOption{withoutLocker: true})

    output, runErr := runRefresh(t, fixture)
    if nil != runErr {
        t.Fatalf("expected the refresh to run without an archive, got %v", runErr)
    }

    if 1 != fixture.sinkHits || 0 != fixture.archive.appended() {
        t.Fatalf("expected one export and no row, got export=%d archive=%d", fixture.sinkHits, fixture.archive.appended())
    }

    if false == strings.Contains(output, "false") {
        t.Fatalf("expected the table to report the archive as not written, got %q", output)
    }
}

/* the archive is the durable half and depends on nothing the sink does: a sink that refuses still takes the exit code, after the row is there — and the operator is told the archive holds what the sink did not receive */
func TestCatalogReportRefreshCommandArchivesTheReadingEvenWhenTheSinkRefusesIt(t *testing.T) {
    fixture := newRefreshFixture(t, refreshFixtureOption{sinkStatus: http.StatusInternalServerError})

    output, runErr := runRefresh(t, fixture)
    if nil == runErr {
        t.Fatalf("expected the sink's refusal to take the exit code")
    }

    if 1 != fixture.archive.appended() {
        t.Fatalf("expected the reading to be archived before the sink was told, got %d rows", fixture.archive.appended())
    }

    if 0 > fixture.sequence.indexOf("archive") || fixture.sequence.indexOf("archive") > fixture.sequence.indexOf("export") {
        t.Fatalf("expected the archive before the export, the run drove %v", fixture.sequence.recorded())
    }

    if false == strings.Contains(output, "the archive holds this reading; the sink did not receive it") {
        t.Fatalf("expected the operator to be told the archive holds the reading, got %q", output)
    }
}

/* the archive's own refusal — the database named with where it is — reaches the console as it is, not under a second headline about the locker */
func TestCatalogReportRefreshCommandHandsBackTheArchivesOwnRefusalUnwrapped(t *testing.T) {
    own := exception.NewError("the archive database at postgres:1/melody_example_v3 could not be opened", nil, errors.New("connection refused"))
    fixture := newRefreshFixture(t, refreshFixtureOption{
        lockerProvider: func(resolver melodycontainercontract.Resolver) (melodylockcontract.Locker, error) {
            return nil, own
        },
    })

    _, runErr := runRefresh(t, fixture)
    if own != runErr {
        t.Fatalf("expected the archive's own refusal to be handed back as it is, got %v", runErr)
    }
}
