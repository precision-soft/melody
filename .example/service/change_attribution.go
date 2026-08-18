package service

import (
    "github.com/precision-soft/melody/.example/repository"
    melodycontainer "github.com/precision-soft/melody/container"
    melodycontainercontract "github.com/precision-soft/melody/container/contract"
    melodyexception "github.com/precision-soft/melody/exception"
    melodyhttp "github.com/precision-soft/melody/http"
    melodysecurity "github.com/precision-soft/melody/security"
    melodysecuritycontract "github.com/precision-soft/melody/security/contract"
)

/* the scoped namespace is not the service one: a scoped registration of a "service." name is refused at the door — that namespace is the framework's at both lifetimes — so the request-scoped services of an application live under "app." */
const ServiceChangeAttribution = "app.example.change.attribution"

/* ChangeAttribution is the request-scoped answer to who is behind the changes of one request. The actor is read once, when the scope first resolves the service, and every write the request performs shares that answer — the scope owns the attribution the way it owns the request context it is derived from. */
type ChangeAttribution struct {
    actor string
}

func NewChangeAttribution(actor string) *ChangeAttribution {
    return &ChangeAttribution{actor: actor}
}

func (instance *ChangeAttribution) Actor() string {
    return instance.actor
}

/* ChangeAttributionFromResolver builds the attribution off the scope that resolves it. The request context is the door: only the http kernel installs one into a scope, so a console run — whose scope carries a process context instead — is refused here, and the journal falls back to reading its runtime directly. The refusal is an error rather than a panic because the console path is legitimate, not a wiring mistake. */
func ChangeAttributionFromResolver(resolver melodycontainercontract.Resolver) (*ChangeAttribution, error) {
    if nil == melodyhttp.RequestContextFromResolver(resolver) {
        return nil, melodyexception.NewError(
            "change attribution exists only on a request scope; a console run is attributed through its runtime",
            nil,
            nil,
        )
    }

    return NewChangeAttribution(actorFromScopeResolver(resolver)), nil
}

/* actorFromScopeResolver mirrors ActorFromRuntime on a scope resolver: the security context is read tolerantly, and its absence — or a resolution failure — is the system, exactly the verdicts the runtime door reaches, because both share the one fold below them. */
func actorFromScopeResolver(resolver melodycontainercontract.Resolver) string {
    securityContext, securityContextErr := melodycontainer.FromResolver[*melodysecurity.SecurityContext](
        resolver,
        melodysecuritycontract.ServiceSecurityContext,
    )
    if nil != securityContextErr || nil == securityContext {
        return repository.CatalogJournalActorSystem
    }

    return actorFromSecurityContext(securityContext)
}
