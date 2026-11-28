package cron

import (
    "testing"
)

func TestBuiltinTemplatesReturnsCrontabVariantsAndK8s(t *testing.T) {
    templates := BuiltinTemplates()
    if 3 != len(templates) {
        t.Fatalf("expected exactly three builtin templates, got %d", len(templates))
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

    if false == names[TemplateNameK8s] {
        t.Fatalf("expected builtin templates to include %q, got %v", TemplateNameK8s, names)
    }
}

func TestRender_RendersThroughTheCrontabTemplate(t *testing.T) {
    entries := []Entry{
        {
            Name:      "backup:run",
            User:      "apache",
            Binary:    "/usr/local/bin/fakeapp",
            Arguments: []string{"backup:run"},
            Schedule:  &Schedule{Minute: "0", Hour: "2"},
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

func TestTemplateRendersUserColumn_AnswersForTheBuiltins(t *testing.T) {
    if false == defaultCrontabTemplate.RendersUserColumn() {
        t.Fatal("the crontab dialect renders a user column")
    }

    if true == defaultCrontabNoUserTemplate.RendersUserColumn() {
        t.Fatal("the crontab-no-user dialect renders none")
    }

    if true == defaultK8sTemplate.RendersUserColumn() {
        t.Fatal("the k8s dialect renders none")
    }
}
