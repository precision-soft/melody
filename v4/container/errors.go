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

/* ErrTeardownDependencyWasNeverRegistered is the cause of an arming refusal, and once armed of a Register or DeclareTeardownDependency refusal, for a declared teardown dependency naming a service nothing registered under any spelling, and of a DeclareTeardownDependency refusal, armed or not, whose declaring service is not registered. A service registered and never built is not refused. */
var ErrTeardownDependencyWasNeverRegistered = errors.New("a declared teardown dependency names a service that was never registered")

/* ErrTeardownDependencyIsScoped is the cause of a refusal at ArmParallelTeardown, or at a Register or DeclareTeardownDependency after it, for a teardown dependency naming a scoped service by name or type. */
var ErrTeardownDependencyIsScoped = errors.New("a declared teardown dependency names a scoped service, which has no node in the container's teardown graph")

/* ErrTeardownDependencyTypeIsAmbiguous is the cause of a refusal at ArmParallelTeardown, at a Register after it, or at the second registration under the type once armed, for a teardown dependency keyed by a type more than one service is registered under. */
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
