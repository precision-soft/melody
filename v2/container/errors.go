package container

import (
    "errors"
)

/* ErrServiceIdAlreadyRegistered is the cause of a Register refusal for a duplicate service name; classify with errors.Is so callers (the application boot collision report) can tell a duplicate apart from other registration failures. */
var ErrServiceIdAlreadyRegistered = errors.New("service already registered")

/* ErrServiceTypeAlreadyRegistered is the cause of a strict type-registration refusal for a duplicate service type. */
var ErrServiceTypeAlreadyRegistered = errors.New("service type already registered")

/* ErrScopedServiceIdAlreadyRegistered is the cause of a RegisterScoped refusal for a duplicate scoped service name. It is distinct from the container's duplicate so a boot collision report can say which lifetime the name was already taken at. */
var ErrScopedServiceIdAlreadyRegistered = errors.New("scoped service already registered")

/* ErrScopedServiceTypeAlreadyRegistered is the cause of a strict scoped type-registration refusal for a duplicate service type. */
var ErrScopedServiceTypeAlreadyRegistered = errors.New("scoped service type already registered")
