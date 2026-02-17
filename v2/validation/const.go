package validation

const (
	ServiceValidator = "service.validator"

	ConstraintNotBlank     = "notBlank"
	ConstraintEmail        = "email"
	ConstraintMin          = "min"
	ConstraintMax          = "max"
	ConstraintRegex        = "regex"
	ConstraintNumeric      = "numeric"
	ConstraintAlpha        = "alpha"
	ConstraintAlphanumeric = "alphanumeric"

	ErrorInvalidRuleSyntax = "invalidRuleSyntax"
	ErrorUnknownRule       = "unknownRule"
	ErrorNotBlank          = "notBlank"
	ErrorInvalidEmail      = "invalidEmail"
	ErrorMinLength         = "minLength"
	ErrorMaxLength         = "maxLength"
	ErrorInvalidPattern    = "invalidPattern"
	ErrorRegexMismatch     = "regexMismatch"
	ErrorNotNumeric        = "notNumeric"
	ErrorNotAlpha          = "notAlpha"
	ErrorNotAlphanumeric   = "notAlphanumeric"
)
