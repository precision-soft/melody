package contract

type Module interface {
    /* Name is the module's identity in the application: not empty, and registered once; a second module of one name fails the boot */
    Name() string

    Description() string
}

type ModuleProvider interface {
    Modules() []Module
}
