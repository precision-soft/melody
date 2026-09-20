package cron

type Template interface {
    Name() string
    Render(entries []Entry, options RenderOptions) (string, error)
}

/* OwnedTemplate is the optional capability of naming a line every destination this template renders carries, by which a later run recognizes a file as one of its own. It exists for --prune, which has to answer "did I write this?" about a file it is being asked to empty, and the only honest answer comes from the rendered content itself: the marker cannot be injected by the generator, because Render is userland and a comment prefix that is right for a crontab corrupts a format that has no comments.

   A template that implements this must render the exact string it returns as a whole line of its own among the first ten lines of everything Render produces, entries or none: the reconciliation reads each leading line trimmed and asks for equality, so a marker embedded inside a longer line, or one that first appears deeper in the file, proves nothing to it. A template that does not implement it is never pruned — a destination whose ownership cannot be proven belongs to the operator, and emptying it on a guess is the one mistake a reconciliation must not make.

   The line names the APPLICATION as well as the command: the builtin templates render CrontabOwnershipMarker followed by " for " and the application's cli name, because two applications sharing an output directory and rendering one identical line had each other's destinations emptied by the other's sweep. A custom dialect declares a line of its own and, if two applications may share its directory, carries the application's name in it from construction. */
type OwnedTemplate interface {
    OwnershipMarker() string
}

/* applicationOwnedTemplate is the capability, internal to the package, of producing a copy of this template that renders and answers the ownership line of one application. The generator asks it of the template it resolved, once per run, with the application's cli name, and uses the copy for the run's rendering and for its sweep, so the line a destination carries and the line the sweep asks for come from one object. A custom dialect that wants a per-application line carries the name from its construction, the way the readme's example carries its other knobs: the door stays internal until a major can add it to the public contract. The generator derives the copy by the concrete type of each builtin rather than through this interface, which an embedding promotes onto a wrapper that is not a builtin; the interface names the shape the builtins share. */
type applicationOwnedTemplate interface {
    ownedBy(applicationName string) Template
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
