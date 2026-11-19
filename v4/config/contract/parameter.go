package contract

import "time"

type Parameter interface {
    EnvironmentKey() string

    Template() any

    Value() any

    /* IsDefault reports whether the declared default spoke; false for an overridden parameter. */
    IsDefault() bool

    /* IsOverridden reports whether the application rewrote the parameter's template through ApplicationConfig.ParameterOverrides; EnvironmentKey still names the key that fed it. */
    IsOverridden() bool

    IsSecret() bool

    String() string

    MustString() string

    Bool() (bool, error)

    Int() (int, error)

    Float() (float64, error)

    Duration() (time.Duration, error)
}
