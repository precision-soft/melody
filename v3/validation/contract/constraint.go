package contract

type Constraint interface {
    Validate(value any, field string) ValidationError
}

/* ParameterizedConstraint constructs a new immutable, concurrency-safe constraint without mutating the template or retaining the parameter map. Invalid or missing parameters fail closed. WithParams must be a pure function of its parameters: successes and failures are memoized for the process lifetime. Concurrent calls may construct equivalent instances, of which only one is retained, so construction must have no external side effects. */
type ParameterizedConstraint interface {
    Constraint

    WithParams(params map[string]string) (Constraint, error)
}
