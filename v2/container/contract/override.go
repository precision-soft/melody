package contract

type OverrideService interface {
    OverrideInstance(serviceName string, value any) error

    MustOverrideInstance(serviceName string, value any)

    OverrideProtectedInstance(serviceName string, value any) error

    MustOverrideProtectedInstance(serviceName string, value any)
}

/* OverrideOptions carries what an override declares beyond the value itself. */
type OverrideOptions struct {
    /* ClosedWithScope hands the installed value to the scope's teardown. An override belongs by default to whoever installed it and outlives the scope — the http kernel installs the request logger and goes on using it to report the scope's own close failure — so a caller that built the value FOR this scope and has nowhere else to close it says so here. */
    ClosedWithScope bool
}

type OverrideOption func(option *OverrideOptions)

/* OverrideServiceWithOptions is the optional companion of OverrideService. It is a separate interface so the four signatures of OverrideService never move: an installed override is the oldest thing in this package and every framework caller of it predates options. */
type OverrideServiceWithOptions interface {
    OverrideInstanceWithOptions(serviceName string, value any, options ...OverrideOption) error

    MustOverrideInstanceWithOptions(serviceName string, value any, options ...OverrideOption)

    OverrideProtectedInstanceWithOptions(serviceName string, value any, options ...OverrideOption) error

    MustOverrideProtectedInstanceWithOptions(serviceName string, value any, options ...OverrideOption)
}
