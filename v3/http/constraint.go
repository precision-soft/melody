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

/* NewRequirements collects the declared requirements into the map a route option takes. It refuses what it used to drop, because both drops failed OPEN: a requirement missing its pattern — a constant that was never filled in, a pattern read from configuration that resolved to "" — left the parameter with NO constraint at all, so a path segment the developer declared numeric matched anything, and a name declared twice kept whichever entry the argument order happened to put last while the other declaration vanished. The router refuses an uncompilable pattern by name at registration; an absent one is the same class of mistake and is refused here, where the declaration is.

   It takes the pointers the Require* helpers return: the helpers were unusable with the value form, since every call site had to dereference them by hand for a signature that could just as well accept what they hand back. */
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
