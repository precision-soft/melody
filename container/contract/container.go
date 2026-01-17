package contract

type Container interface {
	Registrar

	Resolver

	OverrideService

	ScopeManager

	Names() []string

	Close() error
}
