package validation

const (
    ServiceValidator = "service.validator"

    ErrorInvalidRuleSyntax = "invalidRuleSyntax"
    ErrorUnknownRule       = "unknownRule"
)

const (
    /* Deprecated: use ConstraintAlphaErrorNotAlpha instead. */
    ErrorNotAlpha = ConstraintAlphaErrorNotAlpha

    /* Deprecated: use ConstraintAlphanumericErrorNotAlphanumeric instead. */
    ErrorNotAlphanumeric = ConstraintAlphanumericErrorNotAlphanumeric

    /* Deprecated: use ConstraintEmailErrorInvalidEmail instead. */
    ErrorInvalidEmail = ConstraintEmailErrorInvalidEmail

    /* Deprecated: use ConstraintMaxLength instead. */
    ConstraintMax = ConstraintMaxLength
    /* Deprecated: use ConstraintMaxLengthErrorTooLong instead. */
    ErrorMaxLength = ConstraintMaxLengthErrorTooLong

    /* Deprecated: use ConstraintMinLength instead. */
    ConstraintMin = ConstraintMinLength
    /* Deprecated: use ConstraintMinLengthErrorInsufficientLength instead. */
    ErrorMinLength = ConstraintMinLengthErrorInsufficientLength

    /* Deprecated: use ConstraintNotBlankErrorIsBlank instead. */
    ErrorNotBlank = ConstraintNotBlankErrorIsBlank

    /* Deprecated: use ConstraintNotEmptyErrorEmpty instead. */
    ErrorEmpty = ConstraintNotEmptyErrorEmpty

    /* Deprecated: use ConstraintNumericErrorNotNumeric instead. */
    ErrorNotNumeric = ConstraintNumericErrorNotNumeric

    /* Deprecated: use ConstraintRegexErrorMismatch instead. */
    ErrorRegexMismatch = ConstraintRegexErrorMismatch
    /* Deprecated: use ConstraintRegexErrorInvalidPattern instead. */
    ErrorInvalidPattern = ConstraintRegexErrorInvalidPattern
)
