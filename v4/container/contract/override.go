package contract

/* OverrideOptions is what an override declares beyond the value itself. The zero value is the default: on a scope the value belongs to whoever installed it and outlives the scope; the root container closes the value still installed at its own teardown whatever the options. */
type OverrideOptions struct {
    /* ClosedWithScope hands the installed value to the scope's teardown, for a caller that built it for this scope alone. The root container refuses it: what it installs it closes at its own teardown already. */
    ClosedWithScope bool
}

type OverrideService interface {
    OverrideInstance(serviceName string, value any, options OverrideOptions) error

    MustOverrideInstance(serviceName string, value any, options OverrideOptions)

    OverrideProtectedInstance(serviceName string, value any, options OverrideOptions) error

    MustOverrideProtectedInstance(serviceName string, value any, options OverrideOptions)
}
