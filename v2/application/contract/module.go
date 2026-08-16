package contract

/* Module is one unit of wiring, booted through the optional hook interfaces beside this one (ConfigModule, ParameterModule, ServiceModule, ScopedServiceModule, SecurityModule, EventModule, HttpMiddlewareModule, HttpModule, CliModule), each implemented only where the module has something to register.

The hooks run grouped by hook, not by module: every registered module's instance of one hook runs before any module's next hook, with the modules visited in registration order inside each group. A module ordering itself against a sibling therefore orders against the same hook, never against the sibling's whole boot; the application's own http routes register before any module's.

No framework service exists inside any hook. The container is built after the module phases on purpose: the framework claims a default only for a name no module registered — the logger, the cache backend, the session storage, the firewall — so the modules must have spoken first. A hook that needs a framework service registers a provider and resolves it there, at resolution time, when the container is complete; resolving in the hook body finds an empty container.

A module's identity is its instance: the same instance reached through two providers or two registrations boots once. Name and Description identify it in diagnostics and do not participate in that identity. */
type Module interface {
    Name() string

    Description() string
}

/* ModuleProvider hands the application a set of modules to register. A provider that itself implements Module is registered as that module, its own hooks included, and its children follow it; whatever arrives more than once — through either door — boots once. */
type ModuleProvider interface {
    Modules() []Module
}
