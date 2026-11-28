package cron

/* Template renders the entries of one destination in a dialect. OwnershipMarker names the line every destination it renders for applicationName carries, as a whole line of its own among the first ten lines of everything Render produces, entries or none, by which --prune recognises a file as its own before emptying it; the builtin templates answer CrontabOwnershipMarker, " for " and the name, the bare prefix for an empty name. A template answering a blank marker is never pruned, and --prune refuses to sweep on a marker that does not end in " for " and the application's cli name. RendersUserColumn answers whether the dialect renders a user column, which the generator needs before rendering to decide whether a heartbeat line needs a user. */
type Template interface {
    Name() string
    Render(entries []Entry, options RenderOptions) (string, error)
    OwnershipMarker(applicationName string) string
    RendersUserColumn() bool
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
