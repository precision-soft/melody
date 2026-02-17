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
- Provide built-in constraints (for example `notBlank`, `email`, `min`, `max`, `regex`).
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

- Constraints: [`ConstraintNotBlank`, `ConstraintEmail`, `ConstraintMin`, `ConstraintMax`, `ConstraintRegex`, `ConstraintNumeric`, `ConstraintAlpha`, `ConstraintAlphanumeric`](../../validation/const.go)
- Error codes: [`ErrorInvalidRuleSyntax`, `ErrorUnknownRule`, `ErrorNotBlank`, `ErrorInvalidEmail`, `ErrorMinLength`, `ErrorMaxLength`, `ErrorInvalidPattern`, `ErrorRegexMismatch`, `ErrorNotNumeric`, `ErrorNotAlpha`, `ErrorNotAlphanumeric`](../../validation/const.go)

### Constraint implementations

- [`NotBlank`](../../validation/constraint.go)
- [`Email`](../../validation/constraint.go)
- [`Numeric`](../../validation/constraint.go)
- [`Alpha`](../../validation/constraint.go)
- [`Alphanumeric`](../../validation/constraint.go)
- [`NewMinLength(value int)` / `MinLength`](../../validation/constraint.go)
- [`NewMaxLength(value int)` / `MaxLength`](../../validation/constraint.go)
- [`NewRegex(pattern string)` / `Regex`](../../validation/constraint_regex.go)
