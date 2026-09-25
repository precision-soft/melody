package cron

type Template interface {
    Name() string
    Render(entries []Entry, options RenderOptions) (string, error)
}

/* OwnedTemplate is the optional capability of naming a line every destination this template renders carries, by which --prune recognises a file as its own before emptying it; the marker comes from the rendered content because Render is userland and a comment syntax is dialect-specific. A template that implements it renders the exact string it returns as a whole line of its own among the first ten lines of everything Render produces, entries or none, since the reconciliation compares each trimmed leading line for equality. A template that does not implement it is never pruned. The builtin templates render CrontabOwnershipMarker, " for " and the application's cli name; a custom dialect whose directory two applications may share carries the application's name from construction. */
type OwnedTemplate interface {
    OwnershipMarker() string
}

/* applicationOwnedTemplate is the package-internal capability of producing a copy of this template that renders and answers one application's ownership line; the generator uses the copy for the run's rendering and its sweep, so both lines come from one object. The generator derives the copy by each builtin's concrete type rather than through this interface, which an embedding would promote onto a wrapper; the interface is asserted at compile time only, so a builtin that lost the door fails the build. */
type applicationOwnedTemplate interface {
    ownedBy(applicationName string) Template
}

/* UserColumnTemplate is the optional capability of answering whether this dialect renders a user column, which the generator needs before rendering to decide whether a heartbeat line needs a user. A template that does not implement it is judged by name, which answers only for the builtins, so every registered dialect without a user column says so here. */
type UserColumnTemplate interface {
    RendersUserColumn() bool
}

/* templateRendersUserColumn asks the template itself and falls back to the builtin name for one that does not answer, as a registered replacement of a builtin may not; it answers false for the user-less crontab and the k8s names. */
func templateRendersUserColumn(template Template) bool {
    if userColumnTemplate, isUserColumnTemplate := template.(UserColumnTemplate); true == isUserColumnTemplate {
        return userColumnTemplate.RendersUserColumn()
    }

    return TemplateNameCrontabNoUser != template.Name() && TemplateNameK8s != template.Name()
}

func BuiltinTemplates() []Template {
    return []Template{
        defaultCrontabTemplate,
        defaultCrontabNoUserTemplate,
        defaultK8sTemplate,
    }
}

func Render(entries []Entry, options RenderOptions) (string, error) {
    return defaultCrontabTemplate.Render(entries, options)
}
