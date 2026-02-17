package contract

type ParameterBag interface {
	Set(name string, value any)

	Get(name string) (any, bool)

	Has(name string) bool

	Remove(name string)

	Count() int

	All() map[string]any
}
