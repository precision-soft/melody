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

/* k8s resource names are RFC 1123 DNS labels; a CronJob name is further capped so the generated job/pod name suffixes stay within 63 octets */
const k8sNameMaxLength = 52

/* line terminators are rejected outright with an actionable error; every other value is emitted as a double-quoted YAML scalar (with any remaining control character escaped by yamlQuote), so colons, spaces, and wildcards survive without breaking the document */
var k8sForbiddenCharacters = []ForbiddenCharacter{
    {Char: '\n', Reason: "a literal newline terminates the YAML scalar and corrupts the manifest; remove it at the source"},
    {Char: '\r', Reason: "a carriage return terminates the YAML scalar on parsers that treat CR as a line break; remove it before rendering"},
}

/* schedule fields carry the same line-terminator restriction as every other k8s value, plus a % rejection: % is not a valid character in a cron schedule field, so reject it here with a k8s-appropriate reason rather than emitting a manifest the apiserver refuses */
var k8sScheduleForbiddenCharacters = []ForbiddenCharacter{
    {Char: '%', Reason: "not a valid character in a kubernetes CronJob schedule field; remove it at the source"},
    {Char: '\n', Reason: "a literal newline terminates the YAML scalar and corrupts the manifest; remove it at the source"},
    {Char: '\r', Reason: "a carriage return terminates the YAML scalar on parsers that treat CR as a line break; remove it before rendering"},
}

/* k8sHeaderBlock opens every rendered manifest file with the ownership marker as leading YAML comments, so --prune can reconcile the FILE set of a k8s output directory as it does a crontab one: a stale manifest is emptied down to this header. What that does NOT do — unlike a crontab, which crond re-reads — is retire the CronJob object already applied to a cluster: `kubectl apply -f` over a comments-only file changes nothing, so cluster-level retirement needs `kubectl apply --prune` or an explicit delete in the deployment. */
const k8sHeaderBlock = `# GENERATED FILE
# DO NOT EDIT LOCALLY
` + CrontabOwnershipMarker + `
`

type K8sTemplate struct{}

var defaultK8sTemplate = &K8sTemplate{}

func (instance *K8sTemplate) Name() string {
    return TemplateNameK8s
}

/* OwnershipMarker names the comment line every rendered manifest file opens with; the marker is the generating command's, shared with the crontab dialects, because --prune proves who wrote a file, not which dialect rendered it. */
func (instance *K8sTemplate) OwnershipMarker() string {
    return CrontabOwnershipMarker
}

/* RendersUserColumn answers false: a CronJob manifest has no user column, so the generator must not demand a heartbeat user this dialect could never place. */
func (instance *K8sTemplate) RendersUserColumn() bool {
    return false
}

/* Render renders one batch/v1 CronJob document per entry under a marker-carrying comment header, separated by the YAML document marker; heartbeat options are crontab-only and ignored here */
func (instance *K8sTemplate) Render(entries []Entry, options RenderOptions) (string, error) {
    /* an empty render needs no image: it is what --prune writes into a stale manifest file, and demanding the container image to render zero containers would fail the sweep exactly when the configuration was emptied — the version in which every previously written manifest is stale */
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

    /* yamlQuote iterates runes, so an invalid UTF-8 byte in the image reference would be silently rewritten to U+FFFD — a DIFFERENT image pulled than the one configured; it is refused instead */
    if false == utf8.ValidString(options.Image) {
        return "", exception.NewError(
            "cron: the k8s container image is not valid UTF-8; the manifest would silently rewrite it",
            exceptioncontract.Context{"flag": flagNameImage, "parameter": ParameterImage},
            nil,
        )
    }

    /* the namespace is judged by the grammar the apiserver enforces — an RFC 1123 label — the way the restart policy and the resource names already are: it used to be the one k8s value checked only for line terminators, so a namespace with a dot or an uppercase letter rendered a manifest kubectl apply then refused, after generation had reported success */
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

    /* a CronJob pod template accepts only OnFailure or Never; Always is rejected by the apiserver, so fail here with a clear message instead of emitting a manifest kubectl apply will refuse */
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

    /* distinct command names can sanitize to the same k8s resource name (lowercasing, dash-collapsing, the 52-octet cap); two CronJob documents sharing one metadata.name would let kubectl apply silently overwrite the first, so reject the collision here */
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

/* Render only sees one destination's entries, so it can catch collisions within a single manifest stream; the namespace is one global option, so commands split across several destination files can still sanitize to the same resource name and clash on kubectl apply. The CLI calls this over every entry it is about to write to detect that case before rendering. */
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

    /* the same per-field schedule validation the crontab template applies; embedded whitespace, %, CR or LF are all invalid in a k8s cron schedule too, so reject them with a clear error rather than emitting a broken manifest */
    if scheduleValidationErr := validateScheduleFields(entry, k8sScheduleForbiddenCharacters, RunnerDialectKubernetes); nil != scheduleValidationErr {
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

/* a Command override replaces the image entrypoint (k8s "command"); otherwise the command name plus its arguments are passed as "args" so the image entrypoint (the application binary) runs them in CLI mode. Both arrays refuse EMPTY tokens, not just an all-empty override: in the exec form every element is one argv entry, so an empty entrypoint token or an empty argument reaches the pod verbatim and fails there — a CrashLoopBackOff diagnosed in the cluster for a mistake generation could name. The crontab dialects join tokens with spaces, where an empty token merely vanishes; here it is load-bearing. */
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

        /* yamlQuote iterates runes, so an invalid UTF-8 byte would be silently rewritten to U+FFFD — an argv element changed between the configuration and the manifest; it is refused instead */
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

/* isRfc1123Label mirrors the apiserver's namespace grammar: lowercase alphanumerics and '-', starting and ending alphanumeric, at most 63 characters. */
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

/* a command expanded into several parallel instances yields one Entry per run, all sharing the command name; the k8s template needs a unique metadata.name per CronJob, so a -<index> suffix is appended when InstanceCount > 1. The sanitized base is capped so the base plus the suffix still fits k8sNameMaxLength, keeping the 63-octet headroom intact */
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

    /* the suffix is at most a sign plus the digits of an int, so it can never approach the 52-octet cap; baseMaxLength therefore stays comfortably positive and the slice below is always in range */
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

/* emits value as a double-quoted YAML scalar; the backslash and double quote are escaped, the common control characters get their short YAML escapes, and any other C0/C1 control or DEL is escaped as \xNN while the Unicode line and paragraph separators (which a YAML 1.1 parser treats as line breaks) are escaped as \uNNNN, so a stray non-printable byte never lands raw inside the scalar and trips a strict parser. Printable runes (including multi-byte UTF-8) pass through verbatim */
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
