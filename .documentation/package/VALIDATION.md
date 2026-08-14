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

	"github.com/precision-soft/melody/validation"
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
- A struct whose **promoted json codec is `time.Time`'s** is skipped: such a value is an RFC 3339 string on the wire, `encoding/json` hands a body like that to the embedded time and populates nothing else, so no constraint declared inside the struct could be satisfied by any payload and enforcing one would reject every body the type is able to decode ([`promotesValidationTimeCodec`](../../validation/validator.go)). A tag on the **field holding** such a struct still applies. The promoted codec is resolved by Go's own selector rule — shallowest embedding depth wins, a tie promotes nothing — so a shallower marshaler embed that writes an object leaves the struct validated as the object it is. The skip also requires the type to refuse an object body — a struct that declares its own `UnmarshalJSON` accepting an object keeps its constraints enforced, since what a body can populate is decided by the unmarshaler, not by the marshaler the spec advertises.
- `min`/`max` are **string rune-count** constraints (`MinLength`/`MaxLength`), not numeric range and not byte length. They operate on string fields only and reject any other type with `value must be a string`; a string of multi-byte runes therefore passes a `max` its byte length exceeds, so a byte-denominated downstream limit (a `VARCHAR` sized in bytes) needs its own headroom. Use `greaterThan`/`lessThan` for a numeric range (as the `Age` field above does) and `notEmpty` for collections. A negative bound is refused on **both** doors: the tag door answers `min length parameter must not be negative` and fails the rule closed, and `NewMinLength`/`NewMaxLength` panic — a length is never negative, so a negative bound is a declaration mistake, and the constraints it used to build accepted every value in silence (`min`) or refused every value with a message naming an impossible limit (`max`).
- The string-form constraints — `regex`, `email`, `alpha`, `alphanumeric`, `numeric`, `notBlank`, `min`, `max` — refuse a value that is not a string. A nil pointer and the empty string still pass `regex`/`email`/`alpha`/`alphanumeric`/`numeric`, which is what lets them compose with `notBlank` on optional fields; `notBlank` itself rejects a nil pointer, and `min` counts the empty string as zero runes.
- `greaterThan`/`lessThan` operate on numeric fields only and reject a non-numeric value with `value must be numeric`; a floating-point `NaN` is rejected rather than silently passing the bound (`NaN` compares false against every threshold). The bound is an integer in its entirety: a fractional, suffixed or otherwise non-integer bound (`value=1.5`, `value=1e3`) fails the constraint's construction and the rule reports `invalidRuleSyntax` on every value it reaches.
- A parameterized rule named **without parameters** (`regex`, `max()`, `lessThan`) fails closed with `invalidRuleSyntax` — the registered instance is a template for `WithParams`, never a fallback configuration. The regex constraint additionally refuses an empty pattern (`regex=`), which would otherwise compile to a match-everything expression. A tag that parses to no rule at all (`validate:","`) is refused the same way.
- A rejected rule parameter carries the constraint's own refusal reason under `cause` in the error context, and a tag-syntax error names the comma-separated `part` the parser refused.
- The nesting-depth ceiling counts **indirections**, not payload nesting levels: every pointer, interface, struct-field and collection-element hop costs one of the 64, so a `[]*T` chain spends three per JSON level and the deepest reachable payload nesting is correspondingly shallower than 64.
- A `validate` tag next to `json:"-"` is never enforced: `encoding/json` cannot populate the field, so enforcing its zero value would reject every request. On the direct `Validate` path — a handler-built DTO — that field is skipped all the same; use a field the payload can spell, or validate it in the handler.
- **Where a custom constraint is registered.** `RegisterConstraint` is a method on the resolved `*Validator`, and no module hook can reach a framework service: the container is built after the module phases, so a hook body that resolves the validator finds an empty container ([`application/contract.Module`](../../application/contract/module.go) says so at the door). The two places that work are the **composition root between `Boot()` and `Run()`** — resolve the validator through `ValidatorMustFromContainer(kernel.ServiceContainer())` and register there — and a **service provider** the module registers, which resolves the validator at resolution time and registers the constraint then. The provider door only fires if something resolves that service, so a module whose only contribution is a constraint should say so and be resolved, or leave the registration to the application.
  Registering after `Boot()` is safe for concurrency — the registry is guarded by a mutex — but it is not safe *after serving starts*: a request validating against a rule name that is registered mid-flight sees one answer before and another after. Register everything during boot, in one of the two places above.

## Userland API

### Contracts (`validation/contract`)

#### Types

- **Constraint** (`validation/contract.Constraint`)  
  Implementations validate a single field value.

- **ValidationError** (`validation/contract.ValidationError`)  
  A typed error describing a single validation failure.

### Types

- **validation.Validator**  
  Tag-driven validator that can register constraints.

- **validation.ValidationError**  
  Default `validation/contract.ValidationError` implementation.

- **validation.ValidationErrors**  
  Slice of validation errors returned as `error` by `Validator.Validate`. `Error()` flattens the
  messages into one sorted string; `MarshalJSON` renders the collection as the array it is, each
  element through its own marshaler, which is what lets a log record carry the same per-field
  structure the http response body carries.

### Constructors

- [`validation.NewValidator()`](../../validation/validator.go)
- [`validation.NewValidationError(field, message, code string, context map[string]any)`](../../validation/error.go)

### Registration

- [`(*Validator).RegisterConstraint(name string, constraint validationcontract.Constraint)`](../../validation/validator.go) — adds a constraint under the name a `validate` tag spells. It refuses an empty or untrimmed name, a nil constraint, and a **name already taken** — all four with a panic, since all four are declaration mistakes. The last means a name is claimed once: a builtin cannot be replaced by registering over it, so a custom rule needs a name of its own. A parameterized constraint is registered as a **template**: the registered instance is never used as a fallback configuration, only as the receiver of `WithParams`. See the footgun above for *when* to call it.

### Container access (validation)

- [`const ServiceValidator`](../../validation/const.go)
- [`ValidatorMustFromContainer(serviceContainer containercontract.Container)`](../../validation/service_resolver.go)
- [`ValidatorFromContainer(serviceContainer containercontract.Container) *Validator`](../../validation/service_resolver.go) — returns `nil` rather than an error when the service cannot be resolved; the `Must` variant panics instead

### Constants

- Constraints: [`ConstraintNotBlank`, `ConstraintEmail`, `ConstraintMinLength`, `ConstraintMaxLength`, `ConstraintRegex`, `ConstraintNumeric`, `ConstraintAlpha`, `ConstraintAlphanumeric`, `ConstraintGreaterThan`, `ConstraintLessThan`, `ConstraintNotEmpty`](../../validation)
- Deprecated constraint aliases (kept for compatibility): [`ConstraintMin`, `ConstraintMax`](../../validation/const.go)
- Error codes (core): [`ErrorInvalidRuleSyntax`, `ErrorUnknownRule`, `ErrorNestingDepthExceeded`](../../validation/const.go) — the last is reported against a field nested deeper than the 64-level ceiling recursive validation walks, and only when that field's type could carry a `validate` tag at all — an interface-typed member counts as one, its dynamic type being unknowable statically
- **Rule-declaration faults.** `ErrorUnknownRule`, `ErrorInvalidRuleSyntax` and `ConstraintRegexErrorInvalidPattern` blame the DECLARATION, not the submitted value: a rule the registry does not know, a parameter set the constraint refuses, a tag the parser cannot read, a pattern that does not compile. None of them is reachable from any input a client can send — the struct tag is what is wrong, and it is wrong for every request that route will ever serve. They stay field errors with the codes above, because the validator fails closed on a rule it cannot honour rather than passing the value; what changes is who hears about them. [`IsRuleWiringErrorCode`](../../validation/error.go) names the three, [`ValidationErrors.HasRuleWiringError`](../../validation/error.go) answers whether a collection carries one, and [`ValidationErrors.WithoutRuleWiringContext`](../../validation/error.go) projects the collection onto what a client may see.
  The http error path uses all three: a 400 carrying one of these codes is recorded at **error** rather than the warning a deliberate 4xx earns — the deliberation is exactly what is missing — and the internal context of those entries (`rule`, `params`, `cause`, which name the developer's own typo and the constraint's reason) stays in the record and is stripped from the response body. Every other entry keeps its context in both places, so the bounds a numeric constraint reports still reach the client that has to correct its request.
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
- Deprecated error code aliases (kept for compatibility): [`ErrorNotBlank`, `ErrorInvalidEmail`, `ErrorMinLength`, `ErrorMaxLength`, `ErrorInvalidPattern`, `ErrorRegexMismatch`, `ErrorNotNumeric`, `ErrorNotAlpha`, `ErrorNotAlphanumeric`, `ErrorEmpty`](../../validation/const.go)

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

