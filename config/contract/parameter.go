package contract

type Parameter interface {
    EnvironmentKey() string

    EnvironmentValue() any

    Value() any

    IsDefault() bool

    String() string

    MustString() string

    Bool() (bool, error)

    Int() (int, error)
}
