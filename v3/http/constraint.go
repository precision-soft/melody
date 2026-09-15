package http

import (
    "github.com/precision-soft/melody/v3/exception"
)

const (
    ConstraintAlphaLowercase = "^[a-z]+$"
    ConstraintAlpha          = "^[a-zA-Z]+$"
    ConstraintNumeric        = "^[0-9]+$"
    ConstraintAlphaNumeric   = "^[a-zA-Z0-9]+$"
)

type Requirement struct {
    parameterName string
    pattern       string
}

func NewRequirement(parameterName string, pattern string) *Requirement {
    return &Requirement{
        parameterName: parameterName,
        pattern:       pattern,
    }
}

func (instance *Requirement) ParameterName() string {
    return instance.parameterName
}

func (instance *Requirement) Pattern() string {
    return instance.pattern
}

/* NewRequirements accepts the pointers returned by Require helpers. It refuses empty patterns and duplicate parameter names; router registration validates pattern compilation. */
func NewRequirements(requirements ...*Requirement) map[string]string {
    result := map[string]string{}

    for index, requirement := range requirements {
        if nil == requirement {
            exception.Panic(
                exception.NewError(
                    "route requirement may not be nil",
                    map[string]any{
                        "index": index,
                    },
                    nil,
                ),
            )
        }

        if "" == requirement.parameterName {
            exception.Panic(
                exception.NewError(
                    "route requirement parameter name may not be empty",
                    map[string]any{
                        "index":   index,
                        "pattern": requirement.pattern,
                    },
                    nil,
                ),
            )
        }

        if "" == requirement.pattern {
            exception.Panic(
                exception.NewError(
                    "route requirement pattern may not be empty",
                    map[string]any{
                        "index":         index,
                        "parameterName": requirement.parameterName,
                    },
                    nil,
                ),
            )
        }

        if existingPattern, exists := result[requirement.parameterName]; true == exists {
            exception.Panic(
                exception.NewError(
                    "route requirement declared twice for one parameter",
                    map[string]any{
                        "parameterName":   requirement.parameterName,
                        "existingPattern": existingPattern,
                        "pattern":         requirement.pattern,
                    },
                    nil,
                ),
            )
        }

        result[requirement.parameterName] = requirement.pattern
    }

    return result
}

func RequireAlphaLowercase(parameterName string) *Requirement {
    return NewRequirement(parameterName, ConstraintAlphaLowercase)
}

func RequireAlpha(parameterName string) *Requirement {
    return NewRequirement(parameterName, ConstraintAlpha)
}

func RequireNumeric(parameterName string) *Requirement {
    return NewRequirement(parameterName, ConstraintNumeric)
}

func RequireAlphaNumeric(parameterName string) *Requirement {
    return NewRequirement(parameterName, ConstraintAlphaNumeric)
}
