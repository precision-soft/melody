package contract

type ValidationError interface {
	Field() string

	Message() string

	Code() string

	Context() map[string]any

	Error() string
}
