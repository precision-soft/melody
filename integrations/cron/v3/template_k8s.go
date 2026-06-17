package cron

import (
    "fmt"
    "strconv"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

const TemplateNameK8s = "k8s"

const k8sDefaultRestartPolicy = "OnFailure"

/* @important k8s resource names are RFC 1123 DNS labels; a CronJob name is further capped so the generated job/pod name suffixes stay within 63 octets */
const k8sNameMaxLength = 52

/* @info line terminators are rejected outright with an actionable error; every other value is emitted as a double-quoted YAML scalar (with any remaining control character escaped by yamlQuote), so colons, spaces, and wildcards survive without breaking the document */
var k8sForbiddenChars = []ForbiddenChar{
    {Char: '\n', Reason: "a literal newline terminates the YAML scalar and corrupts the manifest; remove it at the source"},
    {Char: '\r', Reason: "a carriage return terminates the YAML scalar on parsers that treat CR as a line break; remove it before rendering"},
}

/* @info schedule fields carry the same line-terminator restriction as every other k8s value, plus a % rejection: % is not a valid character in a cron schedule field, so reject it here with a k8s-appropriate reason rather than emitting a manifest the apiserver refuses */
var k8sScheduleForbiddenChars = []ForbiddenChar{
    {Char: '%', Reason: "not a valid character in a kubernetes CronJob schedule field; remove it at the source"},
    {Char: '\n', Reason: "a literal newline terminates the YAML scalar and corrupts the manifest; remove it at the source"},
    {Char: '\r', Reason: "a carriage return terminates the YAML scalar on parsers that treat CR as a line break; remove it before rendering"},
}

type K8sTemplate struct{}

var defaultK8sTemplate = &K8sTemplate{}

func (instance *K8sTemplate) Name() string {
    return TemplateNameK8s
}

/* @info renders one batch/v1 CronJob document per entry, separated by the YAML document marker; heartbeat options are crontab-only and ignored here */
func (instance *K8sTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
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

    if validationErr := ValidateNoForbiddenChars([]string{options.Image}, k8sForbiddenChars, "k8s image"); nil != validationErr {
        return "", validationErr
    }

    if "" != options.Namespace {
        if validationErr := ValidateNoForbiddenChars([]string{options.Namespace}, k8sForbiddenChars, "k8s namespace"); nil != validationErr {
            return "", validationErr
        }
    }

    restartPolicy := options.RestartPolicy
    if "" == restartPolicy {
        restartPolicy = k8sDefaultRestartPolicy
    }

    if validationErr := ValidateNoForbiddenChars([]string{restartPolicy}, k8sForbiddenChars, "k8s restart policy"); nil != validationErr {
        return "", validationErr
    }

    /* @info a CronJob pod template accepts only OnFailure or Never; Always is rejected by the apiserver, so fail here with a clear message instead of emitting a manifest kubectl apply will refuse */
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

    documentsWritten := 0

    /* @info distinct command names can sanitize to the same k8s resource name (lowercasing, dash-collapsing, the 52-octet cap); two CronJob documents sharing one metadata.name would let kubectl apply silently overwrite the first, so reject the collision here */
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

/* @info Render only sees one destination's entries, so it can catch collisions within a single manifest stream; the namespace is one global option, so commands split across several destination files can still sanitize to the same resource name and clash on kubectl apply. The CLI calls this over every entry it is about to write to detect that case before rendering. */
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

    /* @info the same per-field schedule validation the crontab template applies; embedded whitespace, %, CR or LF are all invalid in a k8s cron schedule too, so reject them with a clear error rather than emitting a broken manifest */
    if scheduleValidationErr := validateScheduleFields(entry, k8sScheduleForbiddenChars); nil != scheduleValidationErr {
        return "", "", scheduleValidationErr
    }

    schedule := entry.Schedule.Expression()
    if validationErr := ValidateNoForbiddenChars([]string{schedule}, k8sForbiddenChars, fmt.Sprintf("entry %q schedule", entry.Name)); nil != validationErr {
        return "", "", validationErr
    }

    invocationKey, invocationTokens, invocationErr := k8sInvocation(entry)
    if nil != invocationErr {
        return "", "", invocationErr
    }

    if validationErr := ValidateNoForbiddenChars(invocationTokens, k8sForbiddenChars, fmt.Sprintf("entry %q command", entry.Name)); nil != validationErr {
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

/* @info a Command override replaces the image entrypoint (k8s "command"); otherwise the command name plus its arguments are passed as "args" so the image entrypoint (the application binary) runs them in CLI mode */
func k8sInvocation(entry Entry) (string, []string, error) {
    if 0 < len(entry.Command) {
        if "" == strings.Join(entry.Command, "") {
            return "", nil, exception.NewError(
                fmt.Sprintf("cron: entry %q has Command but every token is empty; remove the override or supply a non-empty command", entry.Name),
                exceptioncontract.Context{"entry": entry.Name},
                ErrEntryEmptyCommand,
            )
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

    return "args", entry.Args, nil
}

/* @info a command expanded into several parallel instances yields one Entry per run, all sharing the command name; the k8s template needs a unique metadata.name per CronJob, so a -<index> suffix is appended when InstanceCount > 1. The sanitized base is capped so the base plus the suffix still fits k8sNameMaxLength, keeping the 63-octet headroom intact */
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

    /* @info the suffix is at most a sign plus the digits of an int, so it can never approach the 52-octet cap; baseMaxLength therefore stays comfortably positive and the slice below is always in range */
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

/* @info emits value as a double-quoted YAML scalar; the backslash and double quote are escaped, the common control characters get their short YAML escapes, and any other C0/C1 control or DEL is escaped as \xNN while the Unicode line and paragraph separators (which a YAML 1.1 parser treats as line breaks) are escaped as \uNNNN, so a stray non-printable byte never lands raw inside the scalar and trips a strict parser. Printable runes (including multi-byte UTF-8) pass through verbatim */
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

var _ Template = (*K8sTemplate)(nil)
