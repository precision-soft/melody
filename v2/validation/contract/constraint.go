package contract

type Constraint interface {
    Validate(value any, field string) ValidationError
}

/* ParameterizedConstraint is implemented by constraints that accept tag parameters (for example min(value=2)). WithParams returns a NEW constraint configured from the given parameters and must not mutate the receiver; a rule whose parameters cannot be consumed fails closed. */
type ParameterizedConstraint interface {
    Constraint

    WithParams(params map[string]string) (Constraint, error)
}
