package contract

type Constraint interface {
    Validate(value any, field string) ValidationError
}

/* ParameterizedConstraint is implemented by constraints that accept tag parameters, for example min(value=2). WithParams returns a new constraint and must not mutate the receiver; a rule whose parameters cannot be consumed, or a parameterized rule named without parameters, fails closed. The returned constraint is shared for the process across every request, so it must be immutable and must not retain the params map. WithParams may run concurrently for the same parameters with one result kept, so it has no side effects, and it must be a pure function of its parameters, since refusals are memoized like successes. */
type ParameterizedConstraint interface {
    Constraint

    WithParams(params map[string]string) (Constraint, error)
}
