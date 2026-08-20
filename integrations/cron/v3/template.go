package cron

type Template interface {
    Name() string
    Render(entries []Entry, options RenderOptions) (string, error)
}

/* OwnedTemplate is the optional capability of naming a line every destination this template renders carries, by which a later run recognizes a file as one of its own. It exists for --prune, which has to answer "did I write this?" about a file it is being asked to empty, and the only honest answer comes from the rendered content itself: the marker cannot be injected by the generator, because Render is userland and a comment prefix that is right for a crontab corrupts a format that has no comments.

A template that implements this must include the exact string it returns in everything Render produces, entries or none. A template that does not implement it is never pruned — a destination whose ownership cannot be proven belongs to the operator, and emptying it on a guess is the one mistake a reconciliation must not make. */
type OwnedTemplate interface {
    OwnershipMarker() string
}

/* UserColumnTemplate is the optional capability of answering whether this dialect renders a user column. The generator has to know before it renders, because a heartbeat line placed in a dialect that carries a user column needs a user and one placed in a dialect that carries none never does — so a template that renders no user column is refused for a missing user it could not have used.

A template that does not implement this is judged by name, which answers only for the builtins: every registered dialect that renders no user column has to say so here, or it is treated as one that does. */
type UserColumnTemplate interface {
    RendersUserColumn() bool
}

/* templateRendersUserColumn asks the template itself and falls back to the builtin name for one that does not answer. The builtins all implement the interface, so the name branch serves a registered replacement of one of them that does not; it answers false for both user-less builtin names, because the k8s dialect has no user column any more than the user-less crontab dialect does. */
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
