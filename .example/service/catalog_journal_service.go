package service

import (
    "strings"

    "github.com/precision-soft/melody/.example/repository"
    melodyclockcontract "github.com/precision-soft/melody/clock/contract"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyruntimecontract "github.com/precision-soft/melody/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/security"
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

   The repository handle is optional: without a journal database there is nowhere to keep a journal, and the example goes on working without one rather than refusing every write. Recording is therefore something the application does when it can, and the absence is visible in the report rather than in a failure.

   The handle is lazy on purpose: the journal lives on its own database, and nothing dials it until the first recorded change. A dead journal database is then an error on the write that needed it, not a boot failure of a process that might never write. */
type CatalogJournalService struct {
    journalRepository *melodycontainer.LazyService[repository.CatalogJournalRepository]
    clock             melodyclockcontract.Clock
}

func NewCatalogJournalService(
    journalRepository *melodycontainer.LazyService[repository.CatalogJournalRepository],
    clockInstance melodyclockcontract.Clock,
) *CatalogJournalService {
    return &CatalogJournalService{
        journalRepository: journalRepository,
        clock:             clockInstance,
    }
}

/* Record writes one entry, stamped by the injected clock and attributed to whoever the request was authenticated as. The repository is resolved through the lazy handle here, at the first write that needs it — which is the moment the journal database is dialled. */
func (instance *CatalogJournalService) Record(
    runtimeInstance melodyruntimecontract.Runtime,
    action string,
    subject string,
    subjectId string,
) error {
    if nil == instance.journalRepository {
        return nil
    }

    journalRepository, repositoryErr := instance.journalRepository.Resolve()
    if nil != repositoryErr {
        return repositoryErr
    }

    _, appendErr := journalRepository.Append(
        runtimeInstance.Context(),
        &repository.CatalogJournalEntry{
            Actor:      actorForChange(runtimeInstance),
            Action:     action,
            Subject:    subject,
            SubjectId:  subjectId,
            RecordedAt: instance.clock.Now().UTC(),
        },
    )

    return appendErr
}

/* actorForChange prefers the attribution the request scope resolved once and shares across every write of that request; a runtime without one — a console run, whose scope refuses the scoped provider because no request context lives there — is read directly. Both doors end in the same fold, so the verdicts cannot drift. */
func actorForChange(runtimeInstance melodyruntimecontract.Runtime) string {
    scope := runtimeInstance.Scope()
    if nil != scope {
        attribution, attributionErr := melodycontainer.FromResolver[*ChangeAttribution](
            scope,
            ServiceChangeAttribution,
        )
        if nil == attributionErr && nil != attribution {
            return attribution.Actor()
        }
    }

    return ActorFromRuntime(runtimeInstance)
}

/* ActorFromRuntime names whoever is behind the change. A scheduled command and a console run carry no security context at all, and an unauthenticated request carries one with nothing in it; both are the system rather than a person, and the journal says so instead of leaving the column empty. */
func ActorFromRuntime(runtimeInstance melodyruntimecontract.Runtime) string {
    securityContext, found := melodysecurity.SecurityContextFromRuntime(runtimeInstance)
    if false == found {
        return repository.CatalogJournalActorSystem
    }

    return actorFromSecurityContext(securityContext)
}

/* actorFromSecurityContext is the one fold every attribution door shares: the scoped provider and the runtime fallback both end here, which is what keeps their verdicts identical by construction. */
func actorFromSecurityContext(securityContext *melodysecurity.SecurityContext) string {
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
