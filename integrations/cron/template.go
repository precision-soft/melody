package cron

type Template interface {
    Name() string
    Render(entries []Entry, options RenderOptions) (string, error)
}

/* OwnedTemplate exposes a template identification marker for consumers. The generator retains this capability for compatibility, but --prune selects explicit configured destinations and does not inspect markers. */
type OwnedTemplate interface {
    OwnershipMarker() string
}

/* UserColumnTemplate is the optional capability of answering whether this dialect renders a user column. The generator has to know before it renders, because a heartbeat line placed in a dialect that carries a user column needs a user and one placed in a dialect that carries none never does — so a template that renders no user column is refused for a missing user it could not have used.

   A template that does not implement this is judged by name, which answers only for the builtins: every registered dialect that renders no user column has to say so here, or it is treated as one that does. */
type UserColumnTemplate interface {
    RendersUserColumn() bool
}

/* templateRendersUserColumn asks the template itself and falls back to the builtin name for one that does not answer. */
func templateRendersUserColumn(template Template) bool {
    if userColumnTemplate, isUserColumnTemplate := template.(UserColumnTemplate); true == isUserColumnTemplate {
        return userColumnTemplate.RendersUserColumn()
    }

    return TemplateNameCrontabNoUser != template.Name()
}

func BuiltinTemplates() []Template {
    return []Template{
        defaultCrontabTemplate,
        defaultCrontabNoUserTemplate,
    }
}

func Render(entries []Entry, options RenderOptions) (string, error) {
    return defaultCrontabTemplate.Render(entries, options)
}
