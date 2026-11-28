package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* ValidationContext is what a constraint is told about the value it judges; fields may be added, and a constraint reads the ones it needs. */
type ValidationContext struct {
    /* Runtime is the runtime the validation runs under. */
    Runtime containercontract.Runtime

    /* Field is the path of the value from the validated root, as the error reports it. */
    Field string

    /* Owner is the struct that holds the field, as a value, never a pointer; a promoted field's owner is the struct that promotes it, and a value with no owning struct has none. */
    Owner any
}

type Constraint interface {
    Validate(validationContext ValidationContext, value any) ValidationError
}

/* ParameterizedConstraint is implemented by constraints that accept tag parameters, for example min(value=2). WithParameters returns a new constraint and must not mutate the receiver; a rule whose parameters cannot be consumed, or a parameterized rule named without parameters, fails closed. The returned constraint is shared for the process across every request, so it must be immutable and must not retain the parameters map. WithParameters may run concurrently for the same parameters with one result kept, so it has no side effects, and it must be a pure function of its parameters, since refusals are memoized like successes. */
type ParameterizedConstraint interface {
    Constraint

    WithParameters(parameters map[string]string) (Constraint, error)
}

/* DeclarationReporter is implemented by a constraint whose declaration can be wrong in a way its Validate reports only per value, a pattern that does not compile; DeclarationError answers that fault, nil when the declaration is sound, and Validator.VerifyType refuses the type declaring it. */
type DeclarationReporter interface {
    DeclarationError() error
}
