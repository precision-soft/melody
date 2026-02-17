package contract

type Configuration interface {
	Get(name string) Parameter

	MustGet(name string) Parameter

	RegisterRuntime(name string, value any)

	Resolve() error

	Cli() CliConfiguration

	Kernel() KernelConfiguration

	Http() HttpConfiguration

	Names() []string
}
