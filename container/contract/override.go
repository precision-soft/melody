package contract

type OverrideService interface {
	OverrideInstance(serviceName string, value any) error

	MustOverrideInstance(serviceName string, value any)

	OverrideProtectedInstance(serviceName string, value any) error

	MustOverrideProtectedInstance(serviceName string, value any)
}
