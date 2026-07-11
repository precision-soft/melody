package container

import (
    "errors"
)

/* ErrServiceIdAlreadyRegistered is the cause of a Register refusal for a duplicate service name; classify with errors.Is so callers (the application boot collision report) can tell a duplicate apart from other registration failures. */
var ErrServiceIdAlreadyRegistered = errors.New("service already registered")

/* ErrServiceTypeAlreadyRegistered is the cause of a strict type-registration refusal for a duplicate service type. */
var ErrServiceTypeAlreadyRegistered = errors.New("service type already registered")
