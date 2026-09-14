package contract

type OverrideService interface {
    OverrideInstance(serviceName string, value any) error

    MustOverrideInstance(serviceName string, value any)

    OverrideProtectedInstance(serviceName string, value any) error

    MustOverrideProtectedInstance(serviceName string, value any)
}

type OverrideOptions struct {
    /* ClosedWithScope transfers teardown ownership to the scope. By default the installer retains ownership, including request loggers needed to report scope-close failures. */
    ClosedWithScope bool
}

type OverrideOption func(option *OverrideOptions)

/* OverrideServiceWithOptions adds optional ownership controls without widening the existing override contract. */
type OverrideServiceWithOptions interface {
    OverrideInstanceWithOptions(serviceName string, value any, options ...OverrideOption) error

    MustOverrideInstanceWithOptions(serviceName string, value any, options ...OverrideOption)

    OverrideProtectedInstanceWithOptions(serviceName string, value any, options ...OverrideOption) error

    MustOverrideProtectedInstanceWithOptions(serviceName string, value any, options ...OverrideOption)
}
