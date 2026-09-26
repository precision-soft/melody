package contract

const (
    ServiceLifetimeContainer = "container"
    ServiceLifetimeScoped    = "scoped"
)

/* ServiceDescription is what a container can say about a registration without running its provider: the name, the owning lifetime, whether an instance exists, and the type from the instance or from the provider's declared return type. */
type ServiceDescription struct {
    Name     string
    Lifetime string
    IsBuilt  bool
    TypeName string
}
