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

func BuiltinTemplates() []Template {
    return []Template{
        defaultCrontabTemplate,
        defaultCrontabNoUserTemplate,
    }
}

func Render(entries []Entry, options RenderOptions) (string, error) {
    return defaultCrontabTemplate.Render(entries, options)
}
