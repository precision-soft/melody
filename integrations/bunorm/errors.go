package bunorm

import "errors"

var ErrNoProviderDefinitions = errors.New("no provider definitions are registered")

var ErrMultipleDefaultProviderDefinitions = errors.New("multiple provider definitions are marked as default")

var ErrProviderDefinitionNameIsRequired = errors.New("provider definition name is required")

var ErrProviderDefinitionNameMustBeUnique = errors.New("provider definition name must be unique")

var ErrProviderDefinitionNotFound = errors.New("provider definition not found")

var ErrProviderIsRequired = errors.New("provider is required")

var ErrResolverIsRequired = errors.New("resolver is required")
