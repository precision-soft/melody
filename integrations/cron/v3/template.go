package cron

type Template interface {
    Name() string
    Render(entries []Entry, options RenderOptions) (string, error)
}

func BuiltinTemplates() []Template {
    return []Template{
        defaultCrontabTemplate,
    }
}

func Render(entries []Entry, options RenderOptions) (string, error) {
    return defaultCrontabTemplate.Render(entries, options)
}
