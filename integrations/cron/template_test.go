package cron

import (
    "testing"
)

func TestBuiltinTemplatesReturnsCrontabVariants(t *testing.T) {
    templates := BuiltinTemplates()
    if 2 != len(templates) {
        t.Fatalf("expected exactly two builtin templates, got %d", len(templates))
    }

    names := make(map[string]bool, len(templates))
    for _, template := range templates {
        names[template.Name()] = true
    }

    if false == names[TemplateNameCrontab] {
        t.Fatalf("expected builtin templates to include %q, got %v", TemplateNameCrontab, names)
    }

    if false == names[TemplateNameCrontabNoUser] {
        t.Fatalf("expected builtin templates to include %q, got %v", TemplateNameCrontabNoUser, names)
    }
}


/* the package-level Render is the door onto the crontab template, so it answers what that template answers rather than a shape of its own. */
func TestRender_RendersThroughTheCrontabTemplate(t *testing.T) {
    entries := []Entry{
        {
            Name:     "backup:run",
            User:     "apache",
            Binary:   "/usr/local/bin/fakeapp",
            Args:     []string{"backup:run"},
            Schedule: &Schedule{Minute: "0", Hour: "2"},
        },
    }

    rendered, renderErr := Render(entries, RenderOptions{})
    if nil != renderErr {
        t.Fatalf("Render: %v", renderErr)
    }

    direct, directErr := defaultCrontabTemplate.Render(entries, RenderOptions{})
    if nil != directErr {
        t.Fatalf("the crontab template: %v", directErr)
    }

    if direct != rendered {
        t.Fatalf("Render must answer what the crontab template answers\n got: %s\nwant: %s", rendered, direct)
    }
}
