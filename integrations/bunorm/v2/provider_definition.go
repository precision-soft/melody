package bunorm

type ProviderDefinition struct {
	Name      string
	Provider  Provider
	Params    ConnectionParams
	IsDefault bool
}
