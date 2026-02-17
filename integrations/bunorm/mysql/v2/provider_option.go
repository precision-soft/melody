package mysql

type ProviderOption func(provider *Provider)

func WithPostBuildHook(hook PostBuildHook) ProviderOption {
	return func(provider *Provider) {
		provider.postBuildHook = hook
	}
}
