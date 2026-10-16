package contract

type OverrideService interface {
    OverrideInstance(serviceName string, value any) error

    MustOverrideInstance(serviceName string, value any)

    OverrideProtectedInstance(serviceName string, value any) error

    MustOverrideProtectedInstance(serviceName string, value any)
}

/* OverrideOptions carries what an override declares beyond the value itself. */
type OverrideOptions struct {
    /* ClosedWithScope hands the installed value to the scope's teardown. By default an override belongs to whoever installed it and outlives the scope. */
    ClosedWithScope bool
}

type OverrideOption func(option *OverrideOptions)

/* OverrideServiceWithOptions is the optional companion of OverrideService, kept apart so its signatures never move. */
type OverrideServiceWithOptions interface {
    OverrideInstanceWithOptions(serviceName string, value any, options ...OverrideOption) error

    MustOverrideInstanceWithOptions(serviceName string, value any, options ...OverrideOption)

    OverrideProtectedInstanceWithOptions(serviceName string, value any, options ...OverrideOption) error

    MustOverrideProtectedInstanceWithOptions(serviceName string, value any, options ...OverrideOption)
}
