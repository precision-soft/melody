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
- Provide built-in constraints (for example `notBlank`, `email`, `min`, `max`, `regex`, `greaterThan`, `notEmpty`).
- Provide a standard `ValidationError` implementation and an aggregate error type (`ValidationErrors`).
- Provide container helpers to resolve a validator instance.

## Container integration

The package defines the validator service id:

- [`ServiceValidator`](../../validation/const.go) (`"service.validator"`)

Resolution helpers:

- [`ValidatorMustFromContainer`](../../validation/service_resolver.go)
- [`ValidatorFromContainer`](../../validation/service_resolver.go)

## Usage

The example below validates a struct using `validate` tags. Constraints are comma-separated. Constraints with parameters use `name(key=value)`.

```go
package main

import (
	"fmt"

	"github.com/precision-soft/melody/v2/validation"
)

type CreateUserInput struct {
	Email string `json:"email" validate:"notBlank,email"`
	Name  string `json:"name" validate:"notBlank,min(value=3),max(value=64)"`
	Age   int    `json:"age" validate:"min(value=1),max(value=130)"`
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
  Slice of validation errors returned as `error` by `Validator.Validate`.

### Constructors

- [`validation.NewValidator()`](../../validation/validator.go)
- [`validation.NewValidationError(field, message, code string, context map[string]any)`](../../validation/error.go)

### Constants

- Constraints: [`ConstraintNotBlank`, `ConstraintEmail`, `ConstraintMinLength`, `ConstraintMaxLength`, `ConstraintRegex`, `ConstraintNumeric`, `ConstraintAlpha`, `ConstraintAlphanumeric`, `ConstraintGreaterThan`, `ConstraintNotEmpty`](../../validation)
- Deprecated constraint aliases (kept for compatibility): [`ConstraintMin`, `ConstraintMax`](../../validation/const.go)
- Error codes (core): [`ErrorInvalidRuleSyntax`, `ErrorUnknownRule`](../../validation/const.go)
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
- [`NewNotEmpty()` / `NotEmpty`](../../validation/constraint_not_empty.go)

