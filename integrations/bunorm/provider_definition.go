package bunorm

type ProviderDefinition struct {
	Name      string
	Provider  Provider
	IsDefault bool
}
