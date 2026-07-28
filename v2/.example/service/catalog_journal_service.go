package service

import (
    "strings"

    "github.com/precision-soft/melody/v2/.example/repository"
    melodyclockcontract "github.com/precision-soft/melody/v2/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v2/container"
    melodycontainercontract "github.com/precision-soft/melody/v2/container/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v2/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v2/security"
)

const (
    ServiceCatalogJournalService = "service.example.catalog.journal.service"

    /* the parts of the nomenclature a journal entry can be about */
    CatalogJournalSubjectProduct  = "product"
    CatalogJournalSubjectCategory = "category"
    CatalogJournalSubjectCurrency = "currency"
    CatalogJournalSubjectUser     = "user"
)

/* CatalogJournalService records what happened to the nomenclature and who did it.

The repository is optional: without a database there is nowhere to keep a journal, and the example goes on working without one rather than refusing every write. Recording is therefore something the application does when it can, and the absence is visible in the report rather than in a failure. */
type CatalogJournalService struct {
    journalRepository repository.CatalogJournalRepository
    clock             melodyclockcontract.Clock
}

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
    if nil == instance.journalRepository {
        return nil
    }

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

/* ActorFromRuntime names whoever is behind the change. A scheduled command and a console run carry no security context at all, and an unauthenticated request carries one with nothing in it; both are the system rather than a person, and the journal says so instead of leaving the column empty. */
func ActorFromRuntime(runtimeInstance melodyruntimecontract.Runtime) string {
    securityContext, found := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
    if false == found {
        return repository.CatalogJournalActorSystem
    }

    token := securityContext.Token()
    if nil == token {
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
