package container

import (
    "errors"
)

/* ErrServiceIdAlreadyRegistered is the cause of a Register refusal for a duplicate service name; classify with errors.Is so callers (the application boot collision report) can tell a duplicate apart from other registration failures. */
var ErrServiceIdAlreadyRegistered = errors.New("service already registered")

/* ErrServiceTypeAlreadyRegistered is the cause of a strict type-registration refusal for a duplicate service type. */
var ErrServiceTypeAlreadyRegistered = errors.New("service type already registered")

/* ErrTeardownDependencyNameIsRequired is the cause of a Register refusal for a WithTeardownDependency naming nothing. An empty name cannot be an edge, and dropping it silently would report a teardown order that was never installed. */
var ErrTeardownDependencyNameIsRequired = errors.New("teardown dependency name is required")

/* ErrTeardownDependencyIsSelf is the cause of a Register refusal for a service declaring a teardown dependency on itself. The teardown walk ignores a self-edge, so the declaration would be inert; it is refused where it is written rather than dropped where it is read. */
var ErrTeardownDependencyIsSelf = errors.New("a service cannot declare a teardown dependency on itself")

/* ErrScopedTeardownDependencyUnsupported is the cause of a RegisterScoped refusal for a WithTeardownDependency. A scope keeps its own teardown graph, recorded per scope from the resolutions that scope actually made, so a declaration written once at registration has no scope to be written into; accepting it silently would install nothing and read as an ordering that holds. */
var ErrScopedTeardownDependencyUnsupported = errors.New("a scoped registration cannot declare a teardown dependency")

/* ErrScopedServiceIdAlreadyRegistered is the cause of a RegisterScoped refusal for a duplicate scoped service name. It is distinct from the container's duplicate so a boot collision report can say which lifetime the name was already taken at. */
var ErrScopedServiceIdAlreadyRegistered = errors.New("scoped service already registered")

/* ErrScopedServiceTypeAlreadyRegistered is the cause of a strict scoped type-registration refusal for a duplicate service type. */
var ErrScopedServiceTypeAlreadyRegistered = errors.New("scoped service type already registered")

/* ErrScopeClosed is the cause of every refusal a scope answers once Close has ended the request it stood for — resolution, registration, and collection alike. Classify with errors.Is: a goroutine that outlives its request sees the same refusal whichever door it reached for, and the alternative callers were left with is matching the message text, which crosses a module boundary and breaks silently the moment the wording changes. The lazy handle's own refusal carries this cause too, so a caller need not know which door memoized the value. */
var ErrScopeClosed = errors.New("scope is closed")

/* ErrContainerClosed is the cause of every refusal the container answers after Close — resolution and both registration doors. It is distinct from ErrScopeClosed so a caller can tell a request that ended from an application that is shutting down: the first is routine and per-request, the second means nothing will be served again. */
var ErrContainerClosed = errors.New("container is closed")
