package container

import (
    "errors"
)

/* ErrServiceIdAlreadyRegistered is the cause of a Register refusal for a duplicate service name; classify with errors.Is so callers (the application boot collision report) can tell a duplicate apart from other registration failures. */
var ErrServiceIdAlreadyRegistered = errors.New("service already registered")

/* ErrServiceTypeAlreadyRegistered is the cause of a strict type-registration refusal for a duplicate service type. */
var ErrServiceTypeAlreadyRegistered = errors.New("service type already registered")

/* ErrTeardownDependencyNameIsRequired identifies a registration refusal for an empty teardown dependency name. */
var ErrTeardownDependencyNameIsRequired = errors.New("teardown dependency name is required")

/* ErrTeardownDependencyTypeIsRequired is the cause of a Register refusal for a WithTeardownDependencyOfType carrying a nil type, the type form of the refusal above: a nil type names no node, and a declaration that names nothing would read as an ordering that holds. */
var ErrTeardownDependencyTypeIsRequired = errors.New("teardown dependency type is required")

/* ErrTeardownDependencyWasNeverRegistered identifies an unknown declared dependency at arming or subsequent registration. Sequential teardown omits unknown edges; parallel teardown refuses them. Registered but unbuilt optional services are valid targets. */
var ErrTeardownDependencyWasNeverRegistered = errors.New("a declared teardown dependency names a service that was never registered")

/* ErrTeardownDependencyIsScoped is the cause of a refusal — at ArmParallelTeardown, or at a Register made after it — for a teardown dependency naming a SCOPED service, by its name or by a type only a scoped registration filed. A scoped service is built and closed by each scope, so it never has a node in the container's teardown graph, and an edge towards it is dropped by the walk exactly as an edge towards a name nobody registered is; under waves that drop is the ordering itself, so it is refused where the author can still fix it. */
var ErrTeardownDependencyIsScoped = errors.New("a declared teardown dependency names a scoped service, which has no node in the container's teardown graph")

/* ErrTeardownDependencyTypeIsAmbiguous identifies a declared dependency type naming multiple services. Parallel teardown refuses it at arming and rejects subsequent registrations that introduce the ambiguity. */
var ErrTeardownDependencyTypeIsAmbiguous = errors.New("a declared teardown dependency names a type more than one service is registered under")

/* ErrTeardownDependencyIsSelf is the cause of a Register refusal for a service declaring a teardown dependency on itself. The teardown walk ignores a self-edge, so the declaration would be inert; it is refused where it is written rather than dropped where it is read. */
var ErrTeardownDependencyIsSelf = errors.New("a service cannot declare a teardown dependency on itself")

/* ErrScopedTeardownDependencyUnsupported is the cause of a RegisterScoped refusal for a WithTeardownDependency. A scope keeps its own teardown graph, recorded per scope from the resolutions that scope actually made, so a declaration written once at registration has no scope to be written into; accepting it silently would install nothing and read as an ordering that holds. */
var ErrScopedTeardownDependencyUnsupported = errors.New("a scoped registration cannot declare a teardown dependency")

/* ErrScopedServiceIdAlreadyRegistered identifies a duplicate scoped service name, distinct from a container-lifetime duplicate. */
var ErrScopedServiceIdAlreadyRegistered = errors.New("scoped service already registered")

/* ErrScopedServiceTypeAlreadyRegistered is the cause of a strict scoped type-registration refusal for a duplicate service type. */
var ErrScopedServiceTypeAlreadyRegistered = errors.New("scoped service type already registered")

/* ErrScopeClosed identifies resolution, registration, collection and lazy-handle refusals after scope close. Classify it with errors.Is. */
var ErrScopeClosed = errors.New("scope is closed")

/* ErrContainerClosed is the cause of every refusal the container answers after Close — resolution and both registration doors. It is distinct from ErrScopeClosed so a caller can tell a request that ended from an application that is shutting down: the first is routine and per-request, the second means nothing will be served again. */
var ErrContainerClosed = errors.New("container is closed")
