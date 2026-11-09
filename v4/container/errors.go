package container

import (
    "errors"
)

/* ErrServiceIdAlreadyRegistered is the cause of a Register refusal for a duplicate service name, and of a RegisterScoped refusal for a name the container holds unless the scoped registration declares ReplacesContainerService. */
var ErrServiceIdAlreadyRegistered = errors.New("service already registered")

/* ErrServiceTypeAlreadyRegistered is the cause of a strict type-registration refusal for a duplicate service type. */
var ErrServiceTypeAlreadyRegistered = errors.New("service type already registered")

/* ErrTeardownDependencyNameIsRequired is the cause of a Register refusal for a TeardownDependencyNames entry naming nothing. */
var ErrTeardownDependencyNameIsRequired = errors.New("teardown dependency name is required")

/* ErrTeardownDependencyTypeIsRequired is the cause of a Register refusal for a TeardownDependencyTypes entry carrying a nil type. */
var ErrTeardownDependencyTypeIsRequired = errors.New("teardown dependency type is required")

/* ErrTeardownDependencyWasNeverRegistered is the cause of a VerifyDeclaredTeardownDependencies refusal, and of the refusal Close reports, under the waves, for a strict declared teardown dependency naming a service nothing registered under any spelling, and of a DeclareTeardownDependency refusal whose declaring service is not registered. A service registered and never built is not refused. */
var ErrTeardownDependencyWasNeverRegistered = errors.New("a declared teardown dependency names a service that was never registered")

/* ErrTeardownDependencyIsScoped is the cause of a refusal at Register, DeclareTeardownDependency, VerifyDeclaredTeardownDependencies or Close, under the waves, for a teardown dependency naming a scoped service by name or type. */
var ErrTeardownDependencyIsScoped = errors.New("a declared teardown dependency names a scoped service, which has no node in the container's teardown graph")

/* ErrTeardownDependencyTypeIsAmbiguous is the cause of a refusal, under the waves, at a Register declaring a type more than one service is registered under, at the second registration under a type a declaration names, and at VerifyDeclaredTeardownDependencies or Close. */
var ErrTeardownDependencyTypeIsAmbiguous = errors.New("a declared teardown dependency names a type more than one service is registered under")

/* ErrTeardownDependencyIsSelf is the cause of a Register refusal for a service declaring a teardown dependency on itself. */
var ErrTeardownDependencyIsSelf = errors.New("a service cannot declare a teardown dependency on itself")

/* ErrScopedCloserUnsupported is the cause of a scoped registration's refusal for a Closer: a scope closes what it built through the doors the values carry. */
var ErrScopedCloserUnsupported = errors.New("a scoped registration cannot declare a closer")

/* ErrCloserTypeMismatch is the cause of a registration's refusal for a Closer that takes a type the provider's declared type is not assignable to, or that names only one of its two fields, and of the close-time failure of a closer handed a value it does not take. */
var ErrCloserTypeMismatch = errors.New("the closer does not take the service's type")

/* ErrScopedTeardownDependencyUnsupported is the cause of a RegisterScoped refusal for a declared teardown dependency: a scope keeps its own teardown graph. */
var ErrScopedTeardownDependencyUnsupported = errors.New("a scoped registration cannot declare a teardown dependency")

/* ErrScopedServiceIdAlreadyRegistered is the cause of a RegisterScoped refusal for a duplicate scoped service name, and of a container Register refusal for a name a scoped registration holds without ReplacesContainerService; the container's own duplicate has a cause of its own. */
var ErrScopedServiceIdAlreadyRegistered = errors.New("scoped service already registered")

/* ErrScopedServiceTypeAlreadyRegistered is the cause of a strict scoped type-registration refusal for a duplicate service type, and of a container Register refusal, strict or not, for a type a scoped registration holds. */
var ErrScopedServiceTypeAlreadyRegistered = errors.New("scoped service type already registered")

/* ErrScopeClosed is the cause of every refusal a closed scope answers — resolution, registration and collection — and of a lazy handle's refusal over it. */
var ErrScopeClosed = errors.New("scope is closed")

/* ErrContainerClosed is the cause of every refusal the container answers after Close, distinct from ErrScopeClosed. */
var ErrContainerClosed = errors.New("container is closed")

/* ErrCreationWaitersExceeded is the cause of a resolution's refusal when ContainerConfig.CreationWaiters resolutions already wait on the creation it would wait on. */
var ErrCreationWaitersExceeded = errors.New("too many resolutions are waiting on the service's creation")

/* ErrCircularDependency is the cause of every refusal that would close a ring of dependencies: a provider resolving what resolves it — on one chain, across concurrent creations, or through a Lazy handle or a retained resolver after its owner was built — and a declared teardown dependency closing a ring with the edges already recorded. */
var ErrCircularDependency = errors.New("circular service dependency")

/* ErrCloseDoorRequired is the cause of a refusal for a service that carries Close and not CloseWithContext, with no registration Closer taking it: refused at the registration whose provider declares such a concrete type, at an override installing such a value the container or the scope would close, and at the creation of such a value behind a provider declaring an interface, its Close called once so it does not leak. The teardown hands every close its remaining budget, which a plain Close cannot take. */
var ErrCloseDoorRequired = errors.New("a service that closes must take the teardown's context")

/* ErrLazyHandleNameNotRegistered is the cause of a VerifyLazyNames refusal for a Lazy or LazyByType handle whose name or type no container registration answers. */
var ErrLazyHandleNameNotRegistered = errors.New("a lazy handle names a service nothing registered in the container")

/* ErrLazyHandleTypeIsAmbiguous is the cause of a VerifyLazyNames refusal for a LazyByType handle over a type more than one service is registered under. */
var ErrLazyHandleTypeIsAmbiguous = errors.New("a lazy handle names a type more than one service is registered under")

/* ErrLazyHandleTypeMismatch is the cause of a VerifyLazyNames refusal for a Lazy or LazyByType handle whose service's built value or declared concrete type is not the type the handle asserts. */
var ErrLazyHandleTypeMismatch = errors.New("a lazy handle asserts a type its service is not")
