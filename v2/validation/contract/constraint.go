package contract

type Constraint interface {
	Validate(value any, field string) ValidationError
}
