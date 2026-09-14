package contract

const (
    ServiceLifetimeContainer = "container"
    ServiceLifetimeScoped    = "scoped"
)

/* ServiceDescription describes a registration without running its provider. Type comes from the built instance or, before construction, the provider’s return type. */
type ServiceDescription struct {
    Name     string
    Lifetime string
    IsBuilt  bool
    TypeName string
}
