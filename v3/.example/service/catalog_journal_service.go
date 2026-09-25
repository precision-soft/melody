package service

import (
    "context"
    "strings"

    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    examplesecurity "github.com/precision-soft/melody/v3/.example/security"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

const (
    ServiceCatalogJournalService = "service.example.catalog.journal.service"

    CatalogJournalSubjectProduct  = "product"
    CatalogJournalSubjectCategory = "category"
    CatalogJournalSubjectCurrency = "currency"
    CatalogJournalSubjectUser     = "user"
)

/* CatalogJournalService records what happened to the nomenclature and who did it. Its repository always records, in rows with a database and inside the process without one, so no caller asks whether recording worked. */
type CatalogJournalService struct {
    journalRepository repository.CatalogJournalRepository
    clock             melodyclockcontract.Clock
}

//melody:service ServiceCatalogJournalService
func NewCatalogJournalService(
    journalRepository repository.CatalogJournalRepository,
    clockInstance melodyclockcontract.Clock,
) *CatalogJournalService {
    return &CatalogJournalService{
        journalRepository: journalRepository,
        clock:             clockInstance,
    }
}

/* Record writes one entry, stamped by the injected clock and attributed to whoever the request was authenticated as. */
func (instance *CatalogJournalService) Record(
    runtimeInstance melodyruntimecontract.Runtime,
    action string,
    subject string,
    subjectId string,
) error {
    _, appendErr := instance.journalRepository.Append(
        runtimeInstance.Context(),
        &repository.CatalogJournalEntry{
            Actor:      ActorFromRuntime(runtimeInstance),
            Action:     action,
            Subject:    subject,
            SubjectId:  subjectId,
            RecordedAt: instance.clock.Now().UTC(),
        },
    )

    return appendErr
}

/* ActorFromRuntime names whoever is behind the change. A scheduled command, a console run and an unauthenticated request are recorded as the system rather than as an empty actor. */
func ActorFromRuntime(runtimeInstance melodyruntimecontract.Runtime) string {
    token, found := examplesecurity.TokenFromRuntime(runtimeInstance)
    if false == found {
        return repository.CatalogJournalActorSystem
    }

    if false == token.IsAuthenticated() {
        return repository.CatalogJournalActorSystem
    }

    identifier := strings.TrimSpace(token.UserIdentifier())
    if "" == identifier {
        return repository.CatalogJournalActorSystem
    }

    return identifier
}

func MustGetCatalogJournalService(resolver melodycontainercontract.Resolver) *CatalogJournalService {
    return melodycontainer.MustFromResolver[*CatalogJournalService](resolver, ServiceCatalogJournalService)
}

/* WriteContext is the context a change to the nomenclature is made under, carrying who makes it, so a repository that receives a context rather than a runtime can still name a person in the audit trail. The actor travels as a plain string under a key the persistence package owns, which keeps the ORM's actor helper out of the service layer. */
func WriteContext(runtimeInstance melodyruntimecontract.Runtime) context.Context {
    return persistence.WithActor(runtimeInstance.Context(), ActorFromRuntime(runtimeInstance))
}
