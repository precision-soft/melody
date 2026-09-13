package contract

const (
    ServiceLifetimeContainer = "container"
    ServiceLifetimeScoped    = "scoped"
)

/* ServiceDescription is what a container can say about a registration without running its provider: the name, which lifetime owns it, whether an instance already exists, and the type — read from the built instance when there is one, from the provider's declared return type otherwise. It exists so an introspection command can list a container without building it. */
type ServiceDescription struct {
    Name     string
    Lifetime string
    IsBuilt  bool
    TypeName string
}
