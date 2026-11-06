package contract

import "time"

type Parameter interface {
    EnvironmentKey() string

    EnvironmentValue() any

    Value() any

    IsDefault() bool

    IsSecret() bool

    String() string

    MustString() string

    Bool() (bool, error)

    Int() (int, error)

    Float() (float64, error)

    Duration() (time.Duration, error)
}
