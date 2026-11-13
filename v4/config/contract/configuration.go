package contract

type Configuration interface {
    Get(name string) Parameter

    MustGet(name string) Parameter

    RegisterRuntime(name string, value any)

    RegisterRuntimeSecret(name string, value any)

    MarkSecret(name string) bool

    Resolve() error

    Cli() CliConfiguration

    Kernel() KernelConfiguration

    Http() HttpConfiguration

    Names() []string
}
