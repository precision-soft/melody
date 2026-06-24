package cron

import (
    "errors"
    "strings"
    "testing"
)

func k8sSampleEntry(name string) Entry {
    return Entry{
        Name: name,
        User: "www-data",
        Args: []string{name},
        Schedule: &Schedule{
            Minute: "0",
            Hour:   "*/6",
        },
    }
}

func TestK8sRenderEmitsCronJobManifest(t *testing.T) {
    entries := []Entry{k8sSampleEntry("product:list")}

    content, err := defaultK8sTemplate.Render(entries, RenderOptions{
        Image:     "registry/curatorium:latest",
        Namespace: "curatorium",
    })
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    expectedFragments := []string{
        "apiVersion: batch/v1",
        "kind: CronJob",
        "name: \"product-list\"",
        "namespace: \"curatorium\"",
        "schedule: \"0 */6 * * *\"",
        "image: \"registry/curatorium:latest\"",
        "restartPolicy: \"OnFailure\"",
        "args:",
        "- \"product:list\"",
    }

    for _, fragment := range expectedFragments {
        if false == strings.Contains(content, fragment) {
            t.Fatalf("expected manifest to contain %q, got:\n%s", fragment, content)
        }
    }

    /* @info CLI mode is driven by args, not by an env var; the manifest must not emit a dead MELODY_CLI env block */
    for _, forbidden := range []string{"env:", "MELODY_CLI"} {
        if true == strings.Contains(content, forbidden) {
            t.Fatalf("expected manifest to omit %q, got:\n%s", forbidden, content)
        }
    }
}

func TestK8sRenderSanitizesCommandNameToDnsLabel(t *testing.T) {
    entries := []Entry{k8sSampleEntry("Outbox:Dispatch_Now")}

    content, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "name: \"outbox-dispatch-now\"") {
        t.Fatalf("expected sanitized DNS-label name, got:\n%s", content)
    }
}

func TestK8sRenderSeparatesMultipleEntriesWithDocumentMarker(t *testing.T) {
    entries := []Entry{
        k8sSampleEntry("machine:liveness"),
        k8sSampleEntry("reconcile:run"),
    }

    content, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if 1 != strings.Count(content, "---\n") {
        t.Fatalf("expected exactly one document separator between two manifests, got:\n%s", content)
    }

    if 2 != strings.Count(content, "kind: CronJob") {
        t.Fatalf("expected two CronJob documents, got:\n%s", content)
    }
}

func TestK8sRenderDefaultsRestartPolicyToOnFailure(t *testing.T) {
    content, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry("app:info")}, RenderOptions{Image: "img"})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "restartPolicy: \"OnFailure\"") {
        t.Fatalf("expected default restartPolicy OnFailure, got:\n%s", content)
    }
}

func TestK8sRenderHonorsCustomRestartPolicy(t *testing.T) {
    content, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry("app:info")}, RenderOptions{
        Image:         "img",
        RestartPolicy: "Never",
    })
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "restartPolicy: \"Never\"") {
        t.Fatalf("expected custom restartPolicy Never, got:\n%s", content)
    }
}

func TestK8sRenderRejectsInvalidRestartPolicy(t *testing.T) {
    _, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry("app:info")}, RenderOptions{
        Image:         "img",
        RestartPolicy: "Always",
    })
    if nil == err {
        t.Fatalf("expected error for a restartPolicy outside {OnFailure, Never}, got nil")
    }

    if false == errors.Is(err, ErrK8sInvalidRestartPolicy) {
        t.Fatalf("expected ErrK8sInvalidRestartPolicy, got: %v", err)
    }
}

func TestK8sRenderOmitsNamespaceWhenEmpty(t *testing.T) {
    content, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry("app:info")}, RenderOptions{Image: "img"})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if true == strings.Contains(content, "namespace:") {
        t.Fatalf("expected no namespace line when namespace is empty, got:\n%s", content)
    }
}

func TestK8sRenderUsesCommandFieldForCommandOverride(t *testing.T) {
    entries := []Entry{
        {
            Name:     "wrapped",
            User:     "www-data",
            Schedule: &Schedule{Minute: "0"},
            Command:  []string{"/opt/melody/app", "wrapped"},
        },
    }

    content, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "command:") {
        t.Fatalf("expected a command override to render the k8s command field, got:\n%s", content)
    }

    if false == strings.Contains(content, "- \"/opt/melody/app\"") || false == strings.Contains(content, "- \"wrapped\"") {
        t.Fatalf("expected command override tokens, got:\n%s", content)
    }
}

func TestK8sRenderReturnsErrorWhenImageMissing(t *testing.T) {
    _, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry("app:info")}, RenderOptions{})
    if nil == err {
        t.Fatalf("expected error when image is empty, got nil")
    }

    if false == strings.Contains(err.Error(), "image") {
        t.Fatalf("expected error to mention the missing image, got: %v", err)
    }
}

func TestK8sRenderReturnsErrorWhenNameSanitizesToEmpty(t *testing.T) {
    _, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry(":::")}, RenderOptions{Image: "img"})
    if nil == err {
        t.Fatalf("expected error when command name yields no usable k8s name, got nil")
    }
}

func TestK8sRenderRejectsCollidingResourceNames(t *testing.T) {
    entries := []Entry{
        k8sSampleEntry("outbox:dispatch"),
        k8sSampleEntry("outbox-dispatch"),
    }

    _, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil == err {
        t.Fatalf("expected error when two commands map to the same k8s resource name, got nil")
    }

    if false == errors.Is(err, ErrK8sDuplicateName) {
        t.Fatalf("expected ErrK8sDuplicateName, got: %v", err)
    }

    if false == strings.Contains(err.Error(), "outbox-dispatch") {
        t.Fatalf("expected error to mention the colliding resource name, got: %v", err)
    }
}

func TestK8sRenderSuffixesNamesForMultiInstanceCommand(t *testing.T) {
    /* @info a command with Instances > 1 expands into several entries sharing one Name; each must become a distinct CronJob rather than colliding on metadata.name */
    entries := []Entry{
        {Name: "outbox:dispatch", User: "www-data", Args: []string{"outbox:dispatch", "--instance-index=1"}, Schedule: &Schedule{Minute: "0"}, InstanceIndex: 1, InstanceCount: 2},
        {Name: "outbox:dispatch", User: "www-data", Args: []string{"outbox:dispatch", "--instance-index=2"}, Schedule: &Schedule{Minute: "0"}, InstanceIndex: 2, InstanceCount: 2},
    }

    content, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil != err {
        t.Fatalf("Render returned unexpected error for a multi-instance command: %v", err)
    }

    for _, fragment := range []string{"name: \"outbox-dispatch-1\"", "name: \"outbox-dispatch-2\""} {
        if false == strings.Contains(content, fragment) {
            t.Fatalf("expected per-instance suffixed name %q, got:\n%s", fragment, content)
        }
    }

    if 2 != strings.Count(content, "kind: CronJob") {
        t.Fatalf("expected two CronJob documents for a two-instance command, got:\n%s", content)
    }
}

func TestK8sRenderDoesNotSuffixSingleInstanceName(t *testing.T) {
    /* @info InstanceCount 0 (literal entries) and 1 are both single-instance and must not carry a -<index> suffix */
    for _, instanceCount := range []int{0, 1} {
        entry := Entry{Name: "app:info", User: "www-data", Args: []string{"app:info"}, Schedule: &Schedule{Minute: "0"}, InstanceIndex: 1, InstanceCount: instanceCount}

        content, err := defaultK8sTemplate.Render([]Entry{entry}, RenderOptions{Image: "img"})
        if nil != err {
            t.Fatalf("Render returned unexpected error (InstanceCount=%d): %v", instanceCount, err)
        }

        if false == strings.Contains(content, "name: \"app-info\"") {
            t.Fatalf("expected unsuffixed name for single instance (InstanceCount=%d), got:\n%s", instanceCount, content)
        }

        if true == strings.Contains(content, "app-info-1") {
            t.Fatalf("single-instance command must not be suffixed (InstanceCount=%d), got:\n%s", instanceCount, content)
        }
    }
}

func TestK8sRenderRejectsNewlineInImage(t *testing.T) {
    _, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry("app:info")}, RenderOptions{Image: "img\nmalicious: true"})
    if nil == err {
        t.Fatalf("expected error when image contains a newline, got nil")
    }
}

func TestK8sRenderRejectsWhitespaceInScheduleField(t *testing.T) {
    entries := []Entry{
        {
            Name:     "app:info",
            User:     "www-data",
            Args:     []string{"app:info"},
            Schedule: &Schedule{Minute: "0 30", Hour: "*"},
        },
    }

    _, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil == err {
        t.Fatalf("expected error when a schedule field contains whitespace, got nil")
    }

    if false == strings.Contains(err.Error(), "Schedule.Minute") {
        t.Fatalf("expected error to mention the offending field, got: %v", err)
    }
}

func TestK8sRenderRejectsPercentInScheduleField(t *testing.T) {
    /* @info % is invalid in a cron schedule field; the k8s template must reject it with a k8s-appropriate reason, not the crontab line-continuation wording */
    entries := []Entry{
        {
            Name:     "app:info",
            User:     "www-data",
            Args:     []string{"app:info"},
            Schedule: &Schedule{Minute: "0%5", Hour: "*"},
        },
    }

    _, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil == err {
        t.Fatalf("expected error when a schedule field contains %%, got nil")
    }

    if false == errors.Is(err, ErrForbiddenCharacter) {
        t.Fatalf("expected ErrForbiddenCharacter, got: %v", err)
    }

    if true == strings.Contains(err.Error(), "crontab") {
        t.Fatalf("k8s schedule error must not mention crontab, got: %v", err)
    }
}

func TestK8sRenderQuotesScheduleAndPreservesExpression(t *testing.T) {
    entries := []Entry{
        {
            Name:     "app:info",
            User:     "www-data",
            Args:     []string{"app:info"},
            Schedule: &Schedule{Minute: "*/15", Hour: "9-17", DayOfWeek: "mon-fri"},
        },
    }

    content, err := defaultK8sTemplate.Render(entries, RenderOptions{Image: "img"})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "schedule: \"*/15 9-17 * * mon-fri\"") {
        t.Fatalf("expected quoted schedule preserving the cron expression, got:\n%s", content)
    }
}

func TestYamlQuoteEscapesSpecialAndControlCharacters(t *testing.T) {
    cases := []struct {
        name  string
        value string
        want  string
    }{
        {"backslash", "a\\b", "\"a\\\\b\""},
        {"doubleQuote", "a\"b", "\"a\\\"b\""},
        {"tab", "a\tb", "\"a\\tb\""},
        {"null", "a\x00b", "\"a\\0b\""},
        {"lowControl", "a\x01b", "\"a\\x01b\""},
        {"delete", "a\x7fb", "\"a\\x7Fb\""},
        {"c1Control", "ab", "\"a\\x85b\""},
        {"lineSeparator", "a b", "\"a\\u2028b\""},
        {"paragraphSeparator", "a b", "\"a\\u2029b\""},
        {"printableUtf8", "café */5", "\"café */5\""},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            if got := yamlQuote(testCase.value); testCase.want != got {
                t.Fatalf("yamlQuote(%q) = %q, want %q", testCase.value, got, testCase.want)
            }
        })
    }
}

func TestK8sRenderEscapesControlCharacterInImage(t *testing.T) {
    /* @info a tab is not a line terminator, so it passes the forbidden-char gate; the manifest must escape it rather than emit a raw control byte inside the scalar */
    content, err := defaultK8sTemplate.Render([]Entry{k8sSampleEntry("app:info")}, RenderOptions{Image: "img\tlatest"})
    if nil != err {
        t.Fatalf("Render returned unexpected error: %v", err)
    }

    if false == strings.Contains(content, "image: \"img\\tlatest\"") {
        t.Fatalf("expected the tab in the image to be escaped, got:\n%s", content)
    }

    if true == strings.Contains(content, "img\tlatest") {
        t.Fatalf("expected no raw tab byte in the manifest, got:\n%s", content)
    }
}
