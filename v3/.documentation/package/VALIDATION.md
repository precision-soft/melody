# VALIDATION

The [`validation`](../../validation) package provides tag-driven struct validation using registered constraints.

## Scope

- Package: [`validation/`](../../validation)
- Subpackage: [`validation/contract/`](../../validation/contract)

## Subpackages

- [`validation/contract`](../../validation/contract)  
  Public contracts for constraints and validation errors.

## Responsibilities

- Provide the [`Validator`](../../validation/validator.go) type that validates exported struct fields based on the `validate` tag.
- Provide built-in constraints (for example `notBlank`, `email`, `min`, `max`, `regex`, `greaterThan`, `lessThan`, `notEmpty`).
- Provide a standard `ValidationError` implementation and an aggregate error type (`ValidationErrors`).
- Provide container helpers to resolve a validator instance.

## Container integration

The package defines the validator service id:

- [`ServiceValidator`](../../validation/const.go) (`"service.validator"`)

Resolution helpers are documented alongside the userland API in the [Container access](#container-access-validation) section.

## Usage

The example below validates a struct using `validate` tags. Constraints are comma-separated. Constraints with parameters use `name(key=value)`.

```go
package main

import (
	"fmt"

	"github.com/precision-soft/melody/v3/validation"
)

type CreateUserInput struct {
	Email string `json:"email" validate:"notBlank,email"`
	Name  string `json:"name" validate:"notBlank,min(value=3),max(value=64)"`
	Age   int    `json:"age" validate:"greaterThan(value=0),lessThan(value=131)"`
}

func validateInput(input CreateUserInput) error {
	validator := validation.NewValidator()

	validationErr := validator.Validate(input)
	if nil == validationErr {
		return nil
	}

	errors, ok := validationErr.(validation.ValidationErrors)
	if false == ok {
		return validationErr
	}

	for _, item := range errors {
		fmt.Printf("%s: %s (%s)\n", item.Field(), item.Message(), item.Code())
	}

	return validationErr
}
```

## Footguns & caveats

- Only exported struct fields are validated.
- `json:"name"` influences the error field name when a non-empty json name is present.
- `validate:"-"` disables validation for a field.
- A struct whose **promoted json codec is `time.Time`'s** is skipped: such a value is an RFC 3339 string on the wire, `encoding/json` hands a body like that to the embedded time and populates nothing else, so no constraint declared inside the struct could be satisfied by any payload and enforcing one would reject every body the type is able to decode ([`promotesValidationTimeCodec`](../../validation/validator.go)). The `openapi` mirror advertises the same value as a `date-time` string with no properties, so the two agree. A tag on the **field holding** such a struct still applies. The promoted codec is resolved by Go's own selector rule — shallowest embedding depth wins, a tie promotes nothing — so a shallower marshaler embed that writes an object leaves the struct validated as the object it is. The skip also requires the type to refuse an object body — a struct that declares its own `UnmarshalJSON` accepting an object keeps its constraints enforced, since what a body can populate is
  decided by the unmarshaler, not by the marshaler the spec advertises.
- `min`/`max` are **string byte-length** constraints (`MinLength`/`MaxLength`), not numeric range and not rune count. They stringify the value and compare `len()`, so on a numeric field they bound the number of digits, not the value — `max(value=130)` on an `int` accepts any value up to 130 bytes long. Use `greaterThan`/`lessThan` for a numeric range (as the `Age` field above does).
- `greaterThan`/`lessThan` operate on numeric fields only and reject a non-numeric value; a floating-point `NaN` is rejected rather than silently passing the bound (`NaN` compares false against every threshold). The bound is an integer (a fractional bound is truncated toward zero), and the `openapi` generator emits the same truncated integer so the published spec matches what the server enforces.

## Userland API

### Contracts (`validation/contract`)

#### Types

- **Constraint** (`validation/contract.Constraint`)  
  Implementations validate a single field value.

- **ValidationError** (`validation/contract.ValidationError`)  
  A typed error describing a single validation failure.

- **ParameterizedConstraint** (`validation/contract.ParameterizedConstraint`) ([`validation/contract/constraint.go`](../../validation/contract/constraint.go))  
  What a constraint implements to accept tag parameters such as `min(value=2)`. `WithParams` answers a NEW constraint configured from them and must not mutate the receiver; a rule whose parameters cannot be consumed fails closed, and so does a rule that names a parameterized constraint without parameters — the registered instance is a template for `WithParams`, never a fallback configuration.

### Types

- **validation.Validator**  
  Tag-driven validator that can register constraints.
    - [`Validate(data any) error`](../../validation/validator.go) — the door: it answers `ValidationErrors` when a field fails and nil when none does

- **validation.ValidationError**  
  Default `validation/contract.ValidationError` implementation.
    - [`ToExceptionError() error`](../../validation/error.go) — the same failure as an `exception.Error` carrying `field` and `code` in its loggable context, and the field's own context map under `context`

- **validation.ValidationErrors**  
  Slice of validation errors returned as `error` by `Validator.Validate`.
    - [`HasErrors() bool`](../../validation/error.go)

### Constructors

- [`validation.NewValidator()`](../../validation/validator.go)
- [`validation.NewValidationError(field, message, code string, context map[string]any)`](../../validation/error.go)

### Registration

- [`(*Validator).RegisterConstraint(name string, constraint validationcontract.Constraint)`](../../validation/validator.go) — adds a constraint under the name a `validate` tag spells. It refuses an empty or untrimmed name, a nil constraint, and a **name already taken** — all four with a panic, since all four are declaration mistakes. The last means a name is claimed once: a builtin cannot be replaced by registering over it, so a custom rule needs a name of its own. A parameterized constraint is registered as a **template**: the registered instance is never used as a fallback configuration, only as the receiver of `WithParams`. **Where to call it.** No module hook can reach a framework service — the container is built after the module phases, so a hook body that resolves the validator finds an empty container. The two places that work are the **composition root between `Boot()` and `Run()`**, resolving through `ValidatorMustFromContainer(kernel.ServiceContainer())`, and a **service
  provider** the module registers, which resolves the validator at resolution time and registers the constraint then; that door only fires if something resolves the provider's service. The registry is mutex-guarded, so registering after `Boot()` is safe for concurrency — but not after serving starts, since a request validating against a name registered mid-flight sees one answer before and another after.

### Container access (validation)

- [`const ServiceValidator`](../../validation/const.go)
- [`ValidatorMustFromContainer(serviceContainer containercontract.Container)`](../../validation/service_resolver.go)
- [`ValidatorFromContainer(serviceContainer containercontract.Container) *Validator`](../../validation/service_resolver.go) — returns `nil` rather than an error when the service cannot be resolved; the `Must` variant panics instead

### Constants

- Constraints: [`ConstraintNotBlank`, `ConstraintEmail`, `ConstraintMinLength`, `ConstraintMaxLength`, `ConstraintRegex`, `ConstraintNumeric`, `ConstraintAlpha`, `ConstraintAlphanumeric`, `ConstraintGreaterThan`, `ConstraintLessThan`, `ConstraintNotEmpty`](../../validation)
- Error codes (core): [`ErrorInvalidRuleSyntax`, `ErrorUnknownRule`, `ErrorNestingDepthExceeded`](../../validation/const.go) — the last is reported against a field nested deeper than the 64-level ceiling recursive validation walks, and only when that field's type could carry a `validate` tag at all — an interface-typed member counts as one, its dynamic type being unknowable statically
- Error codes (per-constraint):
    - `notBlank`: [`ConstraintNotBlankErrorIsBlank`](../../validation/constraint_not_blank.go)
    - `email`: [`ConstraintEmailErrorInvalidEmail`](../../validation/constraint_email.go)
    - `min`: [`ConstraintMinLengthErrorInsufficientLength`](../../validation/constraint_min_length.go)
    - `max`: [`ConstraintMaxLengthErrorTooLong`](../../validation/constraint_max_length.go)
    - `regex`: [`ConstraintRegexErrorInvalidPattern`, `ConstraintRegexErrorMismatch`](../../validation/constraint_regex.go)
    - `numeric`: [`ConstraintNumericErrorNotNumeric`](../../validation/constraint_numeric.go)
    - `alpha`: [`ConstraintAlphaErrorNotAlpha`](../../validation/constraint_alpha.go)
    - `alphanumeric`: [`ConstraintAlphanumericErrorNotAlphanumeric`](../../validation/constraint_alphanumeric.go)
    - `greaterThan`: [`ConstraintGreaterThanErrorSmallerThan`](../../validation/constraint_greater_than.go)
    - `lessThan`: [`ConstraintLessThanErrorGreaterThan`](../../validation/constraint_less_than.go)
    - `notEmpty`: [`ConstraintNotEmptyErrorEmpty`](../../validation/constraint_not_empty.go)

### Constraint implementations

- [`NotBlank`](../../validation/constraint_not_blank.go)
- [`Email`](../../validation/constraint_email.go)
- [`Numeric`](../../validation/constraint_numeric.go)
- [`Alpha`](../../validation/constraint_alpha.go)
- [`Alphanumeric`](../../validation/constraint_alphanumeric.go)
- [`NewMinLength(value int)` / `MinLength`](../../validation/constraint_min_length.go)
- [`NewMaxLength(value int)` / `MaxLength`](../../validation/constraint_max_length.go)
- [`NewRegex(pattern string)` / `Regex`](../../validation/constraint_regex.go)
- [`NewGreaterThan(min int)` / `GreaterThan`](../../validation/constraint_greater_than.go)
- [`NewLessThan(max int)` / `LessThan`](../../validation/constraint_less_than.go)
- [`NewNotEmpty()` / `NotEmpty`](../../validation/constraint_not_empty.go)

