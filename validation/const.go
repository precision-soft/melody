package validation

const (
	ServiceValidator = "service.validator"

	ErrorInvalidRuleSyntax = "invalidRuleSyntax"
	ErrorUnknownRule       = "unknownRule"
)

const (
	// ErrorNotAlpha @deprecated
	ErrorNotAlpha = ConstraintAlphaErrorNotAlpha

	// ErrorNotAlphanumeric @deprecated
	ErrorNotAlphanumeric = ConstraintAlphanumericErrorNotAlphanumeric

	// ErrorInvalidEmail @deprecated
	ErrorInvalidEmail = ConstraintEmailErrorInvalidEmail

	// ConstraintMax @deprecated
	ConstraintMax = ConstraintMaxLength
	// ErrorMaxLength @deprecated
	ErrorMaxLength = ConstraintMaxLengthErrorTooLong

	// ConstraintMin @deprecated
	ConstraintMin = ConstraintMinLength
	// ErrorMinLength @deprecated
	ErrorMinLength = ConstraintMinLengthErrorInsufficientLength

	// ErrorNotBlank @deprecated
	ErrorNotBlank = ConstraintNotBlankErrorIsBlank

	// ErrorEmpty @deprecated
	ErrorEmpty = ConstraintNotEmptyErrorEmpty

	// ErrorNotNumeric @deprecated
	ErrorNotNumeric = ConstraintNumericErrorNotNumeric

	// ErrorRegexMismatch @deprecated
	ErrorRegexMismatch = ConstraintRegexErrorMismatch
	// ErrorInvalidPattern @deprecated
	ErrorInvalidPattern = ConstraintRegexErrorInvalidPattern
)
