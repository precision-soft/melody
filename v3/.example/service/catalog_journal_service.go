package service

import (
    "context"
    "strings"

    "github.com/precision-soft/melody/v3/.example/persistence"
    "github.com/precision-soft/melody/v3/.example/repository"
    melodyclockcontract "github.com/precision-soft/melody/v3/clock/contract"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
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

   The repository is always there: with a database it writes rows, and without one it keeps the record inside the process. Recording is therefore not something the application does only when it can, and no caller has to ask whether it worked. */
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

/* WriteContext is the context a change to the nomenclature is made under. It carries who is making it, because a repository is handed a context rather than a runtime and the audit trail still has to name a person.

   This is the last layer that knows which request it is serving, so it is where the answer is put on the context. The value travels as a plain string under a key the persistence package owns, which is what keeps the ORM's own actor helper out of the service layer. */
func WriteContext(runtimeInstance melodyruntimecontract.Runtime) context.Context {
    return persistence.WithActor(runtimeInstance.Context(), ActorFromRuntime(runtimeInstance))
}
