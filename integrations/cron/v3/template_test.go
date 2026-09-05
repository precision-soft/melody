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

type userColumnAnsweringTemplate struct {
    name             string
    rendersUserColum bool
}

func (instance *userColumnAnsweringTemplate) Name() string {
    return instance.name
}

func (instance *userColumnAnsweringTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    return "", nil
}

func (instance *userColumnAnsweringTemplate) RendersUserColumn() bool {
    return instance.rendersUserColum
}

type silentTemplate struct {
    name string
}

func (instance *silentTemplate) Name() string {
    return instance.name
}

func (instance *silentTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    return "", nil
}

/* a template that answers for itself is believed whatever it is called, and one that does not is judged by the builtin name — the only thing the generator can read about a dialect it was handed. Deciding on the name alone made every registered dialect that renders no user column, the readme's own kubernetes example among them, demand a crontab user it would never render. */
func TestTemplateRendersUserColumn_AsksTheTemplateBeforeTheName(t *testing.T) {
    for _, testCase := range []struct {
        name     string
        template Template
        expected bool
    }{
        {
            name:     "a registered dialect answering no is believed under a name that means yes",
            template: &userColumnAnsweringTemplate{name: TemplateNameCrontab, rendersUserColum: false},
            expected: false,
        },
        {
            name:     "a registered dialect answering yes is believed under a name that means no",
            template: &userColumnAnsweringTemplate{name: TemplateNameCrontabNoUser, rendersUserColum: true},
            expected: true,
        },
        {
            name:     "a dialect that does not answer falls back to its name",
            template: &silentTemplate{name: TemplateNameCrontabNoUser},
            expected: false,
        },
        {
            name:     "a silent replacement of the k8s builtin is judged user-less by its name",
            template: &silentTemplate{name: TemplateNameK8s},
            expected: false,
        },
        {
            name:     "a dialect of the integrator's own, silent, is assumed to render one",
            template: &silentTemplate{name: "kubernetes-cronjob"},
            expected: true,
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            if testCase.expected != templateRendersUserColumn(testCase.template) {
                t.Fatalf("renders a user column = %v, want %v", templateRendersUserColumn(testCase.template), testCase.expected)
            }
        })
    }
}

func TestTemplateRendersUserColumn_AnswersForTheBuiltins(t *testing.T) {
    if false == templateRendersUserColumn(defaultCrontabTemplate) {
        t.Fatal("the crontab dialect renders a user column")
    }

    if true == templateRendersUserColumn(defaultCrontabNoUserTemplate) {
        t.Fatal("the crontab-no-user dialect renders none")
    }

    if true == templateRendersUserColumn(defaultK8sTemplate) {
        t.Fatal("the k8s dialect renders none")
    }
}
