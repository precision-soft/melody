package contract

/* Module is one unit of wiring, booted through the optional hook interfaces beside this one, each implemented only where the module has something to register. The hooks run grouped by hook, every module's instance of one hook before any module's next, in registration order. No framework service exists inside any hook: the container is built after the module phases, so a hook that needs one registers a provider that resolves it at resolution time. A module's identity is its instance; Name and Description only identify it in diagnostics. */
type Module interface {
    Name() string

    Description() string
}

/* ModuleProvider hands the application a set of modules to register. A provider that itself implements Module is registered as that module, its own hooks included, and its children follow it; whatever arrives more than once boots once. */
type ModuleProvider interface {
    Modules() []Module
}
