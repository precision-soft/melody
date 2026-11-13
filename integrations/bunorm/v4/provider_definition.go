package bunorm

type ProviderDefinition struct {
    Name       string
    Provider   Provider
    Parameters ConnectionParameters
    IsDefault  bool
}
