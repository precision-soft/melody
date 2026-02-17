package http

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

func NewRequirements(requirements ...Requirement) map[string]string {
	result := map[string]string{}

	for _, requirement := range requirements {
		if "" == requirement.parameterName {
			continue
		}

		if "" == requirement.pattern {
			continue
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
