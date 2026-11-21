package contract

type Module interface {
    /* Name is the module's identity in the application, folded to lower case: not empty, without leading or trailing whitespace, and registered once; a second module of one name in any case fails the boot */
    Name() string

    Description() string
}

type ModuleProvider interface {
    Modules() []Module
}
