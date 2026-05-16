package example

import (
    "fmt"
    "strings"

    melodycron "github.com/precision-soft/melody/integrations/cron/v3"
)

type KubernetesCronjobTemplate struct {
    Namespace string
    Image     string
}

func (instance *KubernetesCronjobTemplate) Name() string {
    return "k8s_cronjob"
}

func (instance *KubernetesCronjobTemplate) Render(entries []melodycron.Entry, options melodycron.RenderOptions) (string, error) {
    forbidden := []melodycron.ForbiddenChar{
        {Char: '\t', Reason: "tabs break YAML indentation in CronJob specs"},
    }

    var builder strings.Builder
    builder.WriteString("apiVersion: batch/v1\nkind: List\nitems:\n")
    for _, entry := range entries {
        if validationErr := melodycron.ValidateNoForbiddenChars(entry.Command, forbidden, "k8s entry "+entry.Name); nil != validationErr {
            return "", validationErr
        }

        fmt.Fprintf(
            &builder,
            "  - kind: CronJob\n    metadata:\n      name: %s\n      namespace: %s\n    spec:\n      schedule: %q\n      jobTemplate:\n        spec:\n          template:\n            spec:\n              containers:\n                - name: app\n                  image: %s\n",
            entry.Name,
            instance.Namespace,
            entry.Schedule.Expression(),
            instance.Image,
        )
    }

    return builder.String(), nil
}

var _ melodycron.Template = (*KubernetesCronjobTemplate)(nil)
