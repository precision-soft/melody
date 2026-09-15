package cron

import (
    "fmt"
    "strconv"
    "strings"
    "unicode/utf8"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

const TemplateNameK8s = "k8s"

const k8sDefaultRestartPolicy = "OnFailure"

const k8sNameMaxLength = 52

var k8sForbiddenCharacters = []ForbiddenCharacter{
    {Char: '\n', Reason: "a literal newline terminates the YAML scalar and corrupts the manifest; remove it at the source"},
    {Char: '\r', Reason: "a carriage return terminates the YAML scalar on parsers that treat CR as a line break; remove it before rendering"},
}

var k8sScheduleForbiddenCharacters = []ForbiddenCharacter{
    {Char: '%', Reason: "not a valid character in a kubernetes CronJob schedule field; remove it at the source"},
    {Char: '\n', Reason: "a literal newline terminates the YAML scalar and corrupts the manifest; remove it at the source"},
    {Char: '\r', Reason: "a carriage return terminates the YAML scalar on parsers that treat CR as a line break; remove it before rendering"},
}

const k8sHeaderBlock = `# GENERATED FILE
# DO NOT EDIT LOCALLY
` + CrontabOwnershipMarker + `
`

type K8sTemplate struct{}

var defaultK8sTemplate = &K8sTemplate{}

func (instance *K8sTemplate) Name() string {
    return TemplateNameK8s
}

/* OwnershipMarker returns the template identification line retained in rendered headers. */
func (instance *K8sTemplate) OwnershipMarker() string {
    return CrontabOwnershipMarker
}

/* RendersUserColumn answers false: a CronJob manifest has no user column, so the generator must not demand a heartbeat user this dialect could never place. */
func (instance *K8sTemplate) RendersUserColumn() bool {
    return false
}

/* Render renders one batch/v1 CronJob document per entry under a marker-carrying comment header, separated by the YAML document marker; heartbeat options are crontab-only and ignored here */
func (instance *K8sTemplate) Render(entries []Entry, options RenderOptions) (string, error) {

    if 0 == len(entries) {
        return k8sHeaderBlock, nil
    }

    if "" == options.Image {
        return "", exception.NewError(
            "cron: the k8s template requires a container image; pass --image or register the melody.cron.k8s.image parameter",
            exceptioncontract.Context{
                "flag":      flagNameImage,
                "parameter": ParameterImage,
            },
            ErrK8sImageMissing,
        )
    }

    if validationErr := ValidateNoForbiddenCharacters([]string{options.Image}, k8sForbiddenCharacters, "k8s image"); nil != validationErr {
        return "", validationErr
    }

    if false == utf8.ValidString(options.Image) {
        return "", exception.NewError(
            "cron: the k8s container image is not valid UTF-8; the manifest would silently rewrite it",
            exceptioncontract.Context{"flag": flagNameImage, "parameter": ParameterImage},
            nil,
        )
    }

    if "" != options.Namespace {
        if false == isRfc1123Label(options.Namespace) {
            return "", exception.NewError(
                fmt.Sprintf("cron: k8s namespace %q is not a valid RFC 1123 label (lowercase alphanumerics and '-', starting and ending alphanumeric, at most 63 characters)", options.Namespace),
                exceptioncontract.Context{
                    "namespace": options.Namespace,
                    "flag":      flagNameNamespace,
                    "parameter": ParameterNamespace,
                },
                ErrK8sInvalidNamespace,
            )
        }
    }

    restartPolicy := options.RestartPolicy
    if "" == restartPolicy {
        restartPolicy = k8sDefaultRestartPolicy
    }

    if validationErr := ValidateNoForbiddenCharacters([]string{restartPolicy}, k8sForbiddenCharacters, "k8s restart policy"); nil != validationErr {
        return "", validationErr
    }

    if "OnFailure" != restartPolicy && "Never" != restartPolicy {
        return "", exception.NewError(
            fmt.Sprintf("cron: k8s restartPolicy %q is invalid; use OnFailure or Never", restartPolicy),
            exceptioncontract.Context{
                "restartPolicy": restartPolicy,
                "flag":          flagNameRestartPolicy,
                "parameter":     ParameterRestartPolicy,
            },
            ErrK8sInvalidRestartPolicy,
        )
    }

    var builder strings.Builder
    builder.WriteString(k8sHeaderBlock)

    documentsWritten := 0

    namesSeen := make(map[string]string, len(entries))

    for _, entry := range entries {
        name, manifest, manifestErr := buildCronJobManifest(entry, options.Image, options.Namespace, restartPolicy)
        if nil != manifestErr {
            return "", manifestErr
        }

        if existing, seen := namesSeen[name]; true == seen {
            return "", newK8sDuplicateNameError(existing, entry.Name, name)
        }

        namesSeen[name] = entry.Name

        if 0 < documentsWritten {
            builder.WriteString("---\n")
        }

        builder.WriteString(manifest)

        documentsWritten++
    }

    return builder.String(), nil
}

func ensureK8sNamesUnique(entries []Entry) error {
    namesSeen := make(map[string]string, len(entries))

    for _, entry := range entries {
        name, nameErr := k8sResourceName(entry.Name, entry.InstanceIndex, entry.InstanceCount)
        if nil != nameErr {
            return nameErr
        }

        if existing, seen := namesSeen[name]; true == seen {
            return newK8sDuplicateNameError(existing, entry.Name, name)
        }

        namesSeen[name] = entry.Name
    }

    return nil
}

func newK8sDuplicateNameError(existing string, current string, name string) error {
    return exception.NewError(
        fmt.Sprintf("cron: commands %q and %q both map to the k8s resource name %q; rename one so each CronJob is unique", existing, current, name),
        exceptioncontract.Context{
            "name":          name,
            "command":       current,
            "conflictsWith": existing,
        },
        ErrK8sDuplicateName,
    )
}

func buildCronJobManifest(entry Entry, image string, namespace string, restartPolicy string) (string, string, error) {
    name, nameErr := k8sResourceName(entry.Name, entry.InstanceIndex, entry.InstanceCount)
    if nil != nameErr {
        return "", "", nameErr
    }

    if scheduleValidationErr := ValidateScheduleFields(entry, k8sScheduleForbiddenCharacters, RunnerDialectKubernetes); nil != scheduleValidationErr {
        return "", "", scheduleValidationErr
    }

    schedule := entry.Schedule.Expression()
    if validationErr := ValidateNoForbiddenCharacters([]string{schedule}, k8sForbiddenCharacters, fmt.Sprintf("entry %q schedule", entry.Name)); nil != validationErr {
        return "", "", validationErr
    }

    invocationKey, invocationTokens, invocationErr := k8sInvocation(entry)
    if nil != invocationErr {
        return "", "", invocationErr
    }

    if validationErr := ValidateNoForbiddenCharacters(invocationTokens, k8sForbiddenCharacters, fmt.Sprintf("entry %q command", entry.Name)); nil != validationErr {
        return "", "", validationErr
    }

    var builder strings.Builder

    builder.WriteString("apiVersion: batch/v1\n")
    builder.WriteString("kind: CronJob\n")
    builder.WriteString("metadata:\n")
    builder.WriteString("  name: " + yamlQuote(name) + "\n")
    if "" != namespace {
        builder.WriteString("  namespace: " + yamlQuote(namespace) + "\n")
    }
    builder.WriteString("spec:\n")
    builder.WriteString("  schedule: " + yamlQuote(schedule) + "\n")
    builder.WriteString("  jobTemplate:\n")
    builder.WriteString("    spec:\n")
    builder.WriteString("      template:\n")
    builder.WriteString("        spec:\n")
    builder.WriteString("          restartPolicy: " + yamlQuote(restartPolicy) + "\n")
    builder.WriteString("          containers:\n")
    builder.WriteString("            - name: " + yamlQuote(name) + "\n")
    builder.WriteString("              image: " + yamlQuote(image) + "\n")
    builder.WriteString("              " + invocationKey + ":\n")
    for _, token := range invocationTokens {
        builder.WriteString("                - " + yamlQuote(token) + "\n")
    }

    return name, builder.String(), nil
}

func k8sInvocation(entry Entry) (string, []string, error) {
    if 0 < len(entry.Command) {
        if tokenErr := refuseEmptyTokens(entry.Name, "Command", entry.Command); nil != tokenErr {
            return "", nil, tokenErr
        }

        return "command", entry.Command, nil
    }

    if 0 == len(entry.Args) {
        return "", nil, exception.NewError(
            fmt.Sprintf("cron: entry %q has no command override and no arguments; nothing to schedule", entry.Name),
            exceptioncontract.Context{"entry": entry.Name},
            ErrEntryEmptyCommand,
        )
    }

    if tokenErr := refuseEmptyTokens(entry.Name, "Args", entry.Args); nil != tokenErr {
        return "", nil, tokenErr
    }

    return "args", entry.Args, nil
}

func refuseEmptyTokens(entryName string, field string, tokens []string) error {
    for index, token := range tokens {
        if "" == strings.TrimSpace(token) {
            return exception.NewError(
                fmt.Sprintf("cron: entry %q has an empty %s token at position %d; every token becomes one argv element in the manifest", entryName, field, index),
                exceptioncontract.Context{"entry": entryName, "field": field, "index": index},
                ErrEntryEmptyCommand,
            )
        }

        if false == utf8.ValidString(token) {
            return exception.NewError(
                fmt.Sprintf("cron: entry %q has a %s token at position %d that is not valid UTF-8; the manifest would silently rewrite it", entryName, field, index),
                exceptioncontract.Context{"entry": entryName, "field": field, "index": index},
                ErrEntryEmptyCommand,
            )
        }
    }

    return nil
}

func isRfc1123Label(value string) bool {
    if 0 == len(value) || 63 < len(value) {
        return false
    }

    for index := 0; index < len(value); index++ {
        character := value[index]

        if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
            continue
        }

        if '-' == character && 0 != index && len(value)-1 != index {
            continue
        }

        return false
    }

    return true
}

func k8sResourceName(commandName string, instanceIndex int, instanceCount int) (string, error) {
    suffix := ""
    if 1 < instanceCount {
        suffix = "-" + strconv.Itoa(instanceIndex)
    }

    var builder strings.Builder

    previousDash := false
    for _, runeValue := range strings.ToLower(commandName) {
        if (runeValue >= 'a' && runeValue <= 'z') || (runeValue >= '0' && runeValue <= '9') {
            builder.WriteRune(runeValue)
            previousDash = false

            continue
        }

        if false == previousDash {
            builder.WriteRune('-')
            previousDash = true
        }
    }

    name := strings.Trim(builder.String(), "-")

    baseMaxLength := k8sNameMaxLength - len(suffix)
    if baseMaxLength < len(name) {
        name = strings.Trim(name[:baseMaxLength], "-")
    }

    if "" == name {
        return "", exception.NewError(
            fmt.Sprintf("cron: command name %q does not contain any alphanumeric character usable in a k8s resource name", commandName),
            exceptioncontract.Context{"commandName": commandName},
            ErrK8sInvalidName,
        )
    }

    return name + suffix, nil
}

func yamlQuote(value string) string {
    var builder strings.Builder
    builder.WriteByte('"')

    for _, runeValue := range value {
        switch runeValue {
        case '\\':
            builder.WriteString("\\\\")
        case '"':
            builder.WriteString("\\\"")
        case '\t':
            builder.WriteString("\\t")
        case '\n':
            builder.WriteString("\\n")
        case '\r':
            builder.WriteString("\\r")
        case 0:
            builder.WriteString("\\0")
        default:
            switch {
            case runeValue < 0x20 || 0x7F == runeValue || (runeValue >= 0x80 && runeValue <= 0x9F):
                builder.WriteString(fmt.Sprintf("\\x%02X", runeValue))
            case 0x2028 == runeValue || 0x2029 == runeValue:
                builder.WriteString(fmt.Sprintf("\\u%04X", runeValue))
            default:
                builder.WriteRune(runeValue)
            }
        }
    }

    builder.WriteByte('"')

    return builder.String()
}

var (
    _ Template           = (*K8sTemplate)(nil)
    _ OwnedTemplate      = (*K8sTemplate)(nil)
    _ UserColumnTemplate = (*K8sTemplate)(nil)
)
