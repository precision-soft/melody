package service

import (
    "context"
    "errors"
    "testing"
    "time"

    "github.com/precision-soft/melody/.example/repository"
    melodyclock "github.com/precision-soft/melody/clock"
    melodycontainer "github.com/precision-soft/melody/container"
    melodyruntime "github.com/precision-soft/melody/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

type collectingJournalRepository struct {
    entries   []*repository.CatalogJournalEntry
    appendErr error
}

func (instance *collectingJournalRepository) Append(ctx context.Context, entry *repository.CatalogJournalEntry) (*repository.CatalogJournalEntry, error) {
    if nil != instance.appendErr {
        return nil, instance.appendErr
    }

    instance.entries = append(instance.entries, entry)

    return entry, nil
}

func (instance *collectingJournalRepository) Latest(ctx context.Context, limit int) ([]*repository.CatalogJournalEntry, error) {
    return instance.entries, nil
}

func (instance *collectingJournalRepository) Count(ctx context.Context) (int, error) {
    return len(instance.entries), nil
}

/* runtimeWithToken hands back a runtime carrying the security context the http kernel publishes, holding the token the caller names. A nil token asks for a context that has one declared and none set, which is a shape the resolution path can produce and the actor has to answer for. */
func runtimeWithToken(t *testing.T, token melodysecuritycontract.Token) melodyruntimecontract.Runtime {
    t.Helper()

    serviceContainer := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "/",
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        nil,
        "",
        "",
        nil,
        nil,
        melodysecurity.SourceGlobal,
        melodysecurity.SourceGlobal,
        melodysecurity.SourceGlobal,
        melodysecurity.SourceGlobal,
        melodysecurity.SourceGlobal,
    )

    melodysecurity.SecurityContextSetOnRuntime(runtimeInstance, melodysecurity.NewSecurityContext(firewall, token))

    return runtimeInstance
}

/* Without a database there is nowhere to keep a journal, and the example goes on working rather than refusing every write: the absence shows up in the report, not as a failure on the write path. */
func TestCatalogJournalServiceRecordsNothingWithoutARepository(t *testing.T) {
    journalService := NewCatalogJournalService(nil, melodyclock.NewFrozenClock(time.Now()))

    err := journalService.Record(
        runtimeWithToken(t, melodysecurity.NewAuthenticatedToken("user-3", []string{"ROLE_ADMIN"})),
        repository.CatalogJournalActionCreated,
        CatalogJournalSubjectProduct,
        "prod-1",
    )

    if nil != err {
        t.Fatalf("a write without a journal was refused: %v", err)
    }
}

func TestCatalogJournalServiceStampsTheEntryWithTheInjectedClockInUtc(t *testing.T) {
    /* a zone east of UTC, so a stamp that skipped the conversion carries a different WALL time and not merely a different offset */
    frozen := time.Date(2026, time.August, 14, 3, 30, 0, 0, time.FixedZone("plus-three", 3*60*60))

    journalRepository := &collectingJournalRepository{}
    journalService := NewCatalogJournalService(journalRepository, melodyclock.NewFrozenClock(frozen))

    err := journalService.Record(
        runtimeWithToken(t, melodysecurity.NewAuthenticatedToken("user-2", []string{"ROLE_EDITOR"})),
        repository.CatalogJournalActionCreated,
        CatalogJournalSubjectProduct,
        "prod-7",
    )
    if nil != err {
        t.Fatalf("record the entry: %v", err)
    }

    if 1 != len(journalRepository.entries) {
        t.Fatalf("expected one entry, got %d", len(journalRepository.entries))
    }

    entry := journalRepository.entries[0]

    if time.UTC != entry.RecordedAt.Location() {
        t.Fatalf("the stamp kept the clock's zone: %v", entry.RecordedAt.Location())
    }

    if false == entry.RecordedAt.Equal(frozen) {
        t.Fatalf("the stamp is not the instant the clock reported: %v", entry.RecordedAt)
    }

    if 0 != entry.RecordedAt.Hour() {
        t.Fatalf("expected the utc hour, got %d", entry.RecordedAt.Hour())
    }

    if "user-2" != entry.Actor {
        t.Fatalf("unexpected actor: %q", entry.Actor)
    }

    if CatalogJournalSubjectProduct != entry.Subject || "prod-7" != entry.SubjectId {
        t.Fatalf("unexpected subject: %q %q", entry.Subject, entry.SubjectId)
    }
}

func TestCatalogJournalServiceReportsAFailingAppend(t *testing.T) {
    journalRepository := &collectingJournalRepository{appendErr: errors.New("the journal is unreachable")}
    journalService := NewCatalogJournalService(journalRepository, melodyclock.NewFrozenClock(time.Now()))

    err := journalService.Record(
        runtimeWithToken(t, melodysecurity.NewAuthenticatedToken("user-2", nil)),
        repository.CatalogJournalActionCreated,
        CatalogJournalSubjectProduct,
        "prod-1",
    )

    if nil == err {
        t.Fatalf("a journal that refused the entry was reported as a success")
    }
}

/* A scheduled command and a console run carry no security context at all, and an unauthenticated request carries one with nothing usable in it. All of them are the system rather than a person, and the journal says so instead of leaving the column empty — which is what lets a reader tell "nobody was signed in" from "the actor was lost". */
func TestActorFromRuntimeFallsBackToTheSystem(t *testing.T) {
    for name, build := range map[string]func(*testing.T) melodyruntimecontract.Runtime{
        "no runtime at all": func(t *testing.T) melodyruntimecontract.Runtime {
            return nil
        },
        "no security context on the scope": func(t *testing.T) melodyruntimecontract.Runtime {
            serviceContainer := melodycontainer.NewContainer()

            return melodyruntime.New(context.Background(), serviceContainer.NewScope(), serviceContainer)
        },
        "a security context holding no token": func(t *testing.T) melodyruntimecontract.Runtime {
            return runtimeWithToken(t, nil)
        },
        "an anonymous token": func(t *testing.T) melodyruntimecontract.Runtime {
            return runtimeWithToken(t, melodysecurity.NewAnonymousToken())
        },
        "an authenticated token with a blank identifier": func(t *testing.T) melodyruntimecontract.Runtime {
            return runtimeWithToken(t, melodysecurity.NewAuthenticatedToken("   ", []string{"ROLE_USER"}))
        },
    } {
        t.Run(name, func(t *testing.T) {
            if repository.CatalogJournalActorSystem != ActorFromRuntime(build(t)) {
                t.Fatalf("unexpected actor: %q", ActorFromRuntime(build(t)))
            }
        })
    }
}

func TestActorFromRuntimeNamesTheSignedInUser(t *testing.T) {
    runtimeInstance := runtimeWithToken(t, melodysecurity.NewAuthenticatedToken("  user-3  ", []string{"ROLE_ADMIN"}))

    if "user-3" != ActorFromRuntime(runtimeInstance) {
        t.Fatalf("unexpected actor: %q", ActorFromRuntime(runtimeInstance))
    }
}
