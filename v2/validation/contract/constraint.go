package contract

type Constraint interface {
    Validate(value any, field string) ValidationError
}

/* ParameterizedConstraint is implemented by constraints that accept tag parameters (for example min(value=2)). WithParams returns a NEW constraint configured from the given parameters and must not mutate the receiver; a rule whose parameters cannot be consumed fails closed.

The returned constraint MUST be immutable and safe for concurrent use: the validator constructs it once per distinct (rule name, parameters) pair and then shares that one instance for the rest of the process, across every request and goroutine that reaches the rule. A constraint that accumulates state in Validate, or that retains the params map it was handed, will leak that state between unrelated requests.

WithParams itself may be called concurrently for the same parameters, and only one of the resulting constraints is kept; the others are discarded, so it must have no side effects outside the value it returns. */
type ParameterizedConstraint interface {
    Constraint

    WithParams(params map[string]string) (Constraint, error)
}
