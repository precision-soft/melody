package contract

type ScopeManager interface {
	NewScope() Scope
}

type Scope interface {
	Resolver

	OverrideService

	Close() error
}
