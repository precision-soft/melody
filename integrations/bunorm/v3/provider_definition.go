package bunorm

type ProviderDefinition struct {
    Name      string
    Provider  Provider
    Params    ConnectionParameters
    IsDefault bool
}
