package contract

type Module interface {
	Name() string

	Description() string
}

type ModuleProvider interface {
	Modules() []Module
}
