package debug

import (
    "encoding/json"
    "errors"
    "fmt"
    "reflect"
    "sort"
    "strings"
    "time"
    "unicode/utf8"

    clicontract "github.com/precision-soft/melody/cli/contract"
    "github.com/precision-soft/melody/cli/output"
    containercontract "github.com/precision-soft/melody/container/contract"
    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type ContainerCommand struct {
}

func (instance *ContainerCommand) Name() string {
    return "debug:container"
}

func (instance *ContainerCommand) Description() string {
    return "List container services"
}

const containerCommandBuildFlagName = "build"

func (instance *ContainerCommand) Flags() []clicontract.Flag {
    return append(
        output.DebugFlags(),
        &clicontract.BoolFlag{
            Name:  containerCommandBuildFlagName,
            Usage: "build every listed service and report the failures with their causes",
            Value: false,
        },
    )
}

/* serviceDescriptionReporter is the door the listing asks for descriptions — asked, not required, so a substituted container that does not implement it keeps a name-only listing instead of failing the command */
type serviceDescriptionReporter interface {
    ServiceDescriptions() []containercontract.ServiceDescription
}

func (instance *ContainerCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    startedAt := time.Now()

    option := output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )

    meta := output.NewMeta(
        instance.Name(),
        commandContext.Args().Slice(),
        option,
        startedAt,
        time.Duration(0),
        output.Version{},
    )

    envelope := output.NewEnvelope(meta)

    serviceContainer := runtimeInstance.Container()

    serviceName := ""
    if 0 < commandContext.Args().Len() {
        serviceName = commandContext.Args().First()
    }

    if "" != serviceName {
        instance.populateSingleService(
            serviceContainer,
            runtimeInstance.Scope(),
            serviceName,
            option,
            &envelope,
        )

        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        return output.Render(commandContext.Writer, envelope, option)
    }

    if true == commandContext.Bool(containerCommandBuildFlagName) {
        instance.populateServiceList(
            serviceContainer,
            runtimeInstance.Scope(),
            option,
            &envelope,
        )
    } else {
        instance.describeServiceList(
            serviceContainer,
            option,
            &envelope,
        )
    }

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer, envelope, option)
}

type containerServiceDescriptionItem struct {
    Name     string `json:"name"`
    Lifetime string `json:"lifetime"`
    IsBuilt  bool   `json:"isBuilt"`
    TypeName string `json:"typeName"`
}

/* describeServiceList is the default listing: it runs no provider. The descriptions carry both lifetimes; a container without the descriptions door is listed by name alone, with a warning naming the limitation instead of a silent narrower answer. */
func (instance *ContainerCommand) describeServiceList(
    serviceContainer containercontract.Container,
    option output.Option,
    envelope *output.Envelope,
) {
    items := ([]containerServiceDescriptionItem)(nil)

    if reporter, ok := serviceContainer.(serviceDescriptionReporter); true == ok {
        descriptions := reporter.ServiceDescriptions()

        items = make([]containerServiceDescriptionItem, 0, len(descriptions))
        for _, description := range descriptions {
            items = append(
                items,
                containerServiceDescriptionItem{
                    Name:     description.Name,
                    Lifetime: description.Lifetime,
                    IsBuilt:  description.IsBuilt,
                    TypeName: description.TypeName,
                },
            )
        }
    } else {
        envelope.AddWarning(
            "debug.noDescriptions",
            "container does not describe its registrations; listing the container-lifetime names alone",
            map[string]any{
                "containerType": fmt.Sprintf("%T", serviceContainer),
            },
        )

        names := serviceContainer.Names()

        items = make([]containerServiceDescriptionItem, 0, len(names))
        for _, name := range names {
            items = append(
                items,
                containerServiceDescriptionItem{
                    Name:     name,
                    Lifetime: containercontract.ServiceLifetimeContainer,
                },
            )
        }
    }

    output.ApplySortOrder(items, option.Order)

    total := len(items)
    items = output.WindowItems(items, option.Limit, option.Offset)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()

        summary := fmt.Sprintf(
            "SERVICES: %d total",
            total,
        )

        if len(items) != total {
            summary = fmt.Sprintf(
                "%s | %d shown",
                summary,
                len(items),
            )
        }

        builder.AddSummaryLine(summary)

        containerBlock := builder.AddBlock(
            "SERVICES (CONTAINER)",
            []string{"name", "state", "type"},
        )

        scopedItems := make([]containerServiceDescriptionItem, 0)

        for _, item := range items {
            if containercontract.ServiceLifetimeScoped == item.Lifetime {
                scopedItems = append(scopedItems, item)
                continue
            }

            containerBlock.AddRow(item.Name, renderServiceDescriptionState(item), item.TypeName)
        }

        if 0 < len(scopedItems) {
            scopedBlock := builder.AddBlock(
                "SERVICES (SCOPED)",
                []string{"name", "state", "type"},
            )

            for _, item := range scopedItems {
                scopedBlock.AddRow(item.Name, renderServiceDescriptionState(item), item.TypeName)
            }
        }

        envelope.Table = builder.Build()

        return
    }

    envelope.Data = output.NewListPayload(
        items,
        total,
        option.Limit,
        option.Offset,
    )
}

func renderServiceDescriptionState(item containerServiceDescriptionItem) string {
    if true == item.IsBuilt {
        return "built"
    }

    return "registered"
}

type containerServiceListItem struct {
    Name             string   `json:"name"`
    TypeName         string   `json:"typeName"`
    ErrorString      string   `json:"error"`
    ErrorCauseChain  []string `json:"errorCauseChain"`
    ErrorContextJson string   `json:"errorContextJson"`
}

type containerServiceDetails struct {
    Name             string   `json:"name"`
    Lifetime         string   `json:"lifetime"`
    TypeName         string   `json:"typeName"`
    ErrorString      string   `json:"error"`
    ErrorCauseChain  []string `json:"errorCauseChain"`
    ErrorContextJson string   `json:"errorContextJson"`
}

func resolveErrorContextJson(resolveErr error, option output.Option) string {
    if nil == resolveErr {
        return emptyErrorContextJsonForFormat(option)
    }

    /* the context is read through the ContextProvider contract rather than the concrete *exception.Error: an HttpException — or any userland error carrying a context — in the resolution chain used to contribute nothing, so its context was silently absent from the one report built to show it */
    var provider exceptioncontract.ContextProvider
    if false == errors.As(resolveErr, &provider) || true == internal.IsNilInterface(provider) {
        return emptyErrorContextJsonForFormat(option)
    }

    contextValue := provider.Context()
    if nil == contextValue {
        return emptyErrorContextJsonForFormat(option)
    }

    /* sanitize BEFORE marshalling: both fallbacks below print the value they were handed, so walking only the happy path would leak exactly the stack and trace entries the noise filter strips whenever json.Marshal or json.Unmarshal fails. The defined exceptioncontract.Context type is converted to its plain map[string]any underlying because the walk matches via value.(map[string]any) first; nested named types are converted inside the tracked walk itself. */
    keepNoiseKeys := 3 <= option.VerbosityLevel
    redactedContext := sanitizeErrorContextValueTracked(map[string]any(contextValue), map[errorContextVisitKey]struct{}{}, 0, keepNoiseKeys)

    normalizedContextBytes, normalizeMarshalErr := json.Marshal(redactedContext)
    if nil != normalizeMarshalErr {
        return unrepresentableErrorContextForFormat(redactedContext, option)
    }

    normalizedContext := (any)(nil)
    normalizeUnmarshalErr := json.Unmarshal(normalizedContextBytes, &normalizedContext)
    if nil != normalizeUnmarshalErr {
        fallbackString := fmt.Sprintf("%s", normalizedContextBytes)

        return truncateErrorContextForFormat(fallbackString, option)
    }

    sanitizedContext := sanitizeErrorContextValueTracked(normalizedContext, map[errorContextVisitKey]struct{}{}, 0, keepNoiseKeys)

    contextJsonBytes, marshalErr := json.Marshal(sanitizedContext)
    if nil != marshalErr {
        return unrepresentableErrorContextForFormat(sanitizedContext, option)
    }

    return truncateErrorContextForFormat(string(contextJsonBytes), option)
}

/* emptyErrorContextJsonForFormat answers "nothing to report" in the grammar of the format asking. The json document declares a string of json, so the absence has to be a parseable one: `.errorContextJson | fromjson` died with "Cannot parse ''" on every healthy row of the very sweep built to be read by a machine, and a field whose type changes with the value of the row cannot be consumed at all. The table keeps the empty cell, where a literal {} would be noise in a column read by a person. */
func emptyErrorContextJsonForFormat(option output.Option) string {
    if output.FormatTable == option.Format {
        return ""
    }

    return "{}"
}

/* unrepresentableErrorContextForFormat answers a context json.Marshal refuses — a chan, a func, a complex, a MarshalJSON that fails — in the grammar of the format asking, the same split emptyErrorContextJsonForFormat makes. The sanitizing walk passes an unrecognised scalar through untouched, so this is reachable from any provider that puts one in a context, and the %v rendering it used to answer is Go syntax: `"errorContextJson": "map[listener:0x5f8e40]"`, on which the `.errorContextJson | fromjson` this line documents dies for that row alone. Wrapping it in an object keeps the field parseable on every row and keeps the rendering, which names the culprit, under a key that says what it is. The table keeps the bare rendering, where a person reads the value rather than parses it. */
func unrepresentableErrorContextForFormat(contextValue any, option output.Option) string {
    rendered := fmt.Sprintf("%v", contextValue)

    if output.FormatTable == option.Format {
        return truncateErrorContextForFormat(rendered, option)
    }

    rawBytes, marshalErr := json.Marshal(
        map[string]string{
            "raw": rendered,
        },
    )
    if nil != marshalErr {
        return emptyErrorContextJsonForFormat(option)
    }

    return string(rawBytes)
}

/* normalizeErrorCauseChain answers an empty list rather than a nil one, so the field stays an array in every row of one document — `jq '.data.items[].errorCauseChain[]'` used to die with "Cannot iterate over null" at the first service that resolved. It is the convention the envelope factory already applies to Warnings. The table renders both spellings identically. */
func normalizeErrorCauseChain(causeChain []string) []string {
    if nil == causeChain {
        return []string{}
    }

    return causeChain
}

/* truncateErrorContextForFormat applies the table-cell truncation to the table format alone: the json envelope is a machine document, and cutting a json fragment at a display width handed the consumer an unparseable value with no sign anything was dropped */
func truncateErrorContextForFormat(value string, option output.Option) string {
    if output.FormatTable != option.Format {
        return value
    }

    return truncateTableCellValueByVerbosity(value, option.VerbosityLevel)
}

/* resolveErrorCauseChain walks the causes below the resolution error's own message, so the report names why the build failed and not only that it did: the error string of a melody error is its message alone, and the dial refusal, the missing file, the refused credential all live below it — the one detail the operator runs the command to learn used to reach neither the table nor the json */
func resolveErrorCauseChain(resolveErr error) []string {
    if nil == resolveErr {
        return nil
    }

    return exception.BuildCauseChain(errors.Unwrap(resolveErr), 8)
}

/* populateServiceList is the --build sweep: every windowed service is resolved and the failures report their causes. A scoped registration resolves through the run's own scope — the scope a console command's services live in — never through the container that refuses it. */
func (instance *ContainerCommand) populateServiceList(
    serviceContainer containercontract.Container,
    runScope containercontract.Scope,
    option output.Option,
    envelope *output.Envelope,
) {
    scopedNames := map[string]struct{}{}
    serviceNames := ([]string)(nil)

    if reporter, ok := serviceContainer.(serviceDescriptionReporter); true == ok {
        descriptions := reporter.ServiceDescriptions()

        serviceNames = make([]string, 0, len(descriptions))
        for _, description := range descriptions {
            serviceNames = append(serviceNames, description.Name)

            if containercontract.ServiceLifetimeScoped == description.Lifetime {
                scopedNames[description.Name] = struct{}{}
            }
        }
    } else {
        serviceNames = serviceContainer.Names()
    }

    sort.Strings(serviceNames)

    output.ApplySortOrder(serviceNames, option.Order)

    total := len(serviceNames)

    selected := output.WindowItems(serviceNames, option.Limit, option.Offset)

    okItems := make([]containerServiceListItem, 0, len(selected))
    errorItems := make([]containerServiceListItem, 0, len(selected))

    for _, name := range selected {
        serviceInstance, getErr := resolveServiceForLifetime(serviceContainer, runScope, name, scopedNames)

        typeName := ""
        errorString := ""
        errorContextJson := emptyErrorContextJsonForFormat(option)
        errorCauseChain := []string{}

        if nil != getErr {
            errorString = getErr.Error()
            errorCauseChain = normalizeErrorCauseChain(resolveErrorCauseChain(getErr))
            errorContextJson = resolveErrorContextJson(getErr, option)
        }

        if nil != serviceInstance {
            typeName = fmt.Sprintf("%T", serviceInstance)
        }

        item := containerServiceListItem{
            Name:             name,
            TypeName:         typeName,
            ErrorString:      errorString,
            ErrorCauseChain:  errorCauseChain,
            ErrorContextJson: errorContextJson,
        }

        if "" != item.ErrorString {
            errorItems = append(errorItems, item)
        } else {
            okItems = append(okItems, item)
        }
    }

    reportServiceSweepFailures(errorItems, envelope)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()
        shown := len(okItems) + len(errorItems)

        summary := fmt.Sprintf(
            "SERVICES: %d total",
            total,
        )

        /* the shown count precedes the ok/error split so the split reads as scoped to it: only the windowed services are resolved, and an unqualified "8 ok | 2 error" beside a larger total implied the rest were neither instead of unprobed */
        if shown != total {
            summary = fmt.Sprintf(
                "%s | %d shown",
                summary,
                shown,
            )
        }

        summary = fmt.Sprintf(
            "%s | %d ok | %d error",
            summary,
            len(okItems),
            len(errorItems),
        )

        builder.AddSummaryLine(summary)

        okBlock := builder.AddBlock(
            "SERVICES (OK)",
            []string{"name", "type"},
        )

        for _, item := range okItems {
            okBlock.AddRow(item.Name, item.TypeName)
        }

        hasAnyType := false
        for _, item := range errorItems {
            if "" != item.TypeName {
                hasAnyType = true
                break
            }
        }

        if 0 < len(errorItems) {
            if true == hasAnyType {
                errorBlock := builder.AddBlock(
                    "SERVICES (ERROR)",
                    []string{"name", "type", "error"},
                )

                for _, item := range errorItems {
                    errorBlock.AddRow(output.TableRowSeparatorToken)

                    rows := buildContainerServiceTableRows(item, option)
                    for _, row := range rows {
                        errorBlock.AddRow(row[0], row[1], row[2])
                    }

                    errorBlock.AddRow(output.TableRowSeparatorToken)
                }
            } else {
                errorBlock := builder.AddBlock(
                    "SERVICES (ERROR)",
                    []string{"name", "error"},
                )

                for _, item := range errorItems {
                    errorBlock.AddRow(output.TableRowSeparatorToken)

                    errorLines := buildContainerServiceErrorLines(item)
                    errorLines = limitLinesByVerbosity(errorLines, option.VerbosityLevel)

                    for index := 0; index < len(errorLines); index++ {
                        nameCell := ""
                        if 0 == index {
                            nameCell = item.Name
                        }

                        errorBlock.AddRow(nameCell, errorLines[index])
                    }

                    errorBlock.AddRow(output.TableRowSeparatorToken)
                }
            }
        }

        envelope.Table = builder.Build()

        return
    }

    combined := make([]containerServiceListItem, 0, len(okItems)+len(errorItems))
    combined = append(combined, okItems...)
    combined = append(combined, errorItems...)

    envelope.Data = output.NewListPayload(
        combined,
        total,
        option.Limit,
        option.Offset,
    )
}

/* reportServiceSweepFailures puts the sweep's failures where the exit code reads them. Render turns an envelope carrying an error into a non-zero exit, which is the whole reason the envelope contract exists: `app debug:container --build --format=json || exit 1` is a deployment gate, and a sweep whose declared purpose is "build everything and report the failures with their causes" used to answer `"error": null` and exit 0 over every one of them, leaving the failures reachable only as data.items[].error for a consumer nobody told to read them. The single-name door and the middleware sibling have reported theirs all along. */
func reportServiceSweepFailures(
    errorItems []containerServiceListItem,
    envelope *output.Envelope,
) {
    if 0 == len(errorItems) {
        return
    }

    failedNames := make([]string, 0, len(errorItems))
    for _, item := range errorItems {
        failedNames = append(failedNames, item.Name)
    }

    envelope.SetError(
        "debug.buildFailed",
        "services failed to build",
        map[string]any{
            "failedCount": len(errorItems),
            "failedNames": failedNames,
        },
        output.NewErrorCause(
            errorItems[0].ErrorString,
            map[string]any{
                "serviceName": errorItems[0].Name,
                "causeChain":  errorItems[0].ErrorCauseChain,
            },
        ),
    )
}

/* resolveServiceForLifetime routes a build to the owner of the name: the container for its own services, the run's scope for a scoped registration — the same resolution a scoped service gets everywhere else in a console process */
func resolveServiceForLifetime(
    serviceContainer containercontract.Container,
    runScope containercontract.Scope,
    serviceName string,
    scopedNames map[string]struct{},
) (any, error) {
    if _, isScoped := scopedNames[serviceName]; true == isScoped && false == internal.IsNilInterface(runScope) {
        return runScope.Get(serviceName)
    }

    return serviceContainer.Get(serviceName)
}

/* the placeholder a container that contains itself is rendered as, so the operator sees where the loop closed instead of a truncated blob or nothing at all */
const errorContextCycleMarker = "<cycle>"

/* errorContextDepthMarker stands in for a subtree the walk refused to descend into. It reads differently from the cycle marker because the two say different things to whoever is looking at the rendered context: a cycle is a structure that closes on itself, this is a structure that simply goes deeper than anything worth printing. */
const errorContextDepthMarker = "<depth limit>"

/* maximumErrorContextDepth bounds the descent. The cycle guard above answers the context that holds itself; it says nothing about one that is merely very deep, and nothing else did either — a deep enough acyclic context walked until the goroutine stack was gone. That failure is `fatal error: stack overflow`, which no recover reaches, so the command layer cannot report it and the process dies rendering a debug page. Measured with the stack capped at 16 MiB it took some five hundred thousand levels, which the production cap of one gigabyte scales up rather than removes.

The bound is far above anything a real error context reaches: these are producer-supplied maps describing a failure, and a hand-built one nests a handful of levels. It matches the bound internal/copy.go puts on the same shape of walk for the same reason. */
const maximumErrorContextDepth = 10000

/* the walk records the containers on the current path only, and drops each one again on the way out. An error context may legitimately hand the same map or slice to two sibling keys, and rendering the second one as a cycle would be a silent wrong answer; a container is only a cycle when it is its own ancestor. A slice is keyed on its backing pointer together with its length, so two views of the same array are told apart rather than collapsed. */
type errorContextVisitKey struct {
    pointer uintptr
    length  uintptr
}

func sanitizeErrorContextValue(value any) any {
    return sanitizeErrorContextValueTracked(value, map[errorContextVisitKey]struct{}{}, 0, false)
}

/* the context handed in at the top of resolveErrorContextJson is the caller's own map, redacted before it reaches json.Marshal so the fallbacks cannot print what the redaction exists to strip. That ordering puts this walk ahead of encoding/json's cycle detector, so the walk carries its own: a context holding itself — `context["self"] = context`, which any producer can build — would otherwise recurse until the stack is gone, and a stack overflow is a fatal error that no recover in the command layer turns into a reported failure. */
/* the plain shapes the tracked walk descends into; a defined type sharing their underlying type is converted to them below, which keeps the backing pointer and so the cycle keying */
var plainContextMapType = reflect.TypeOf(map[string]any(nil))
var plainContextSliceType = reflect.TypeOf([]any(nil))

func sanitizeErrorContextValueTracked(value any, seen map[errorContextVisitKey]struct{}, depth int, keepNoiseKeys bool) any {
    if nil == value {
        return nil
    }

    if maximumErrorContextDepth <= depth {
        return errorContextDepthMarker
    }

    mapValue, isMap := value.(map[string]any)
    if false == isMap {
        /* a defined type whose underlying type is map[string]any — the framework's own exceptioncontract.Context is one, and it is exactly what a producer reaches for when nesting structured data — fails the assertion above while carrying the same shape. Left unconverted it rode past all three guards at once: a cycle survived into json.Marshal, whose cycle error routed it to the fmt fallback that has no cycle detection of its own — a fatal stack overflow no recover reaches — a depth past the bound recursed inside the encoder, and a dropped key inside it reached the fallbacks in the clear. */
        reflectedValue := reflect.ValueOf(value)
        if reflect.Map == reflectedValue.Kind() && true == reflectedValue.Type().ConvertibleTo(plainContextMapType) {
            mapValue = reflectedValue.Convert(plainContextMapType).Interface().(map[string]any)
            isMap = true
        }
    }
    if true == isMap {
        key := errorContextVisitKey{pointer: reflect.ValueOf(mapValue).Pointer()}
        if _, visited := seen[key]; true == visited {
            return errorContextCycleMarker
        }
        seen[key] = struct{}{}
        defer delete(seen, key)

        return sanitizeErrorContextMap(mapValue, seen, depth, keepNoiseKeys)
    }

    sliceValue, isSlice := value.([]any)
    if false == isSlice {
        /* a defined slice type with underlying []any is converted for the reason the map conversion documents */
        reflectedValue := reflect.ValueOf(value)
        if reflect.Slice == reflectedValue.Kind() && true == reflectedValue.Type().ConvertibleTo(plainContextSliceType) {
            sliceValue = reflectedValue.Convert(plainContextSliceType).Interface().([]any)
            isSlice = true
        }
    }
    if true == isSlice {
        pointer := reflect.ValueOf(sliceValue).Pointer()
        if 0 != pointer {
            key := errorContextVisitKey{pointer: pointer, length: uintptr(len(sliceValue)) + 1}
            if _, visited := seen[key]; true == visited {
                return errorContextCycleMarker
            }
            seen[key] = struct{}{}
            defer delete(seen, key)
        }

        return sanitizeErrorContextSlice(sliceValue, seen, depth, keepNoiseKeys)
    }

    return value
}

func sanitizeErrorContextMap(value map[string]any, seen map[errorContextVisitKey]struct{}, depth int, keepNoiseKeys bool) map[string]any {
    result := map[string]any{}

    for key, itemValue := range value {
        if false == keepNoiseKeys && true == shouldDropErrorContextKey(key) {
            continue
        }

        result[key] = sanitizeErrorContextValueTracked(itemValue, seen, depth+1, keepNoiseKeys)
    }

    return result
}

func sanitizeErrorContextSlice(value []any, seen map[errorContextVisitKey]struct{}, depth int, keepNoiseKeys bool) []any {
    result := make([]any, 0, len(value))

    for _, itemValue := range value {
        result = append(result, sanitizeErrorContextValueTracked(itemValue, seen, depth+1, keepNoiseKeys))
    }

    return result
}

func toLowerAscii(value string) string {
    if "" == value {
        return ""
    }

    bytesValue := []byte(value)

    for index := 0; index < len(bytesValue); index++ {
        character := bytesValue[index]

        if character >= 'A' && character <= 'Z' {
            bytesValue[index] = character + ('a' - 'A')
        }
    }

    return string(bytesValue)
}

/* shouldDropErrorContextKey names DIAGNOSTIC NOISE, not secrets: a stack or trace entry floods the table with screens of frames, so it is dropped below full verbosity — and only there, since -vvv shows the context whole. The filter deliberately redacts nothing else: the command prints what a producer put in its context, and a producer that parks a credential there owns that choice, exactly as every producer in this tree already refuses to. */
func shouldDropErrorContextKey(key string) bool {
    if "trace" == key {
        return true
    }
    if "stack" == key {
        return true
    }
    if "stackTrace" == key {
        return true
    }
    if "stacktrace" == key {
        return true
    }
    if "traceString" == key {
        return true
    }
    if "trace_string" == key {
        return true
    }
    if "panicStack" == key {
        return true
    }

    lowerKey := toLowerAscii(key)

    if true == containsSubstring(lowerKey, "trace") {
        return true
    }
    if true == containsSubstring(lowerKey, "stack") {
        return true
    }

    return false
}

func containsSubstring(value string, needle string) bool {
    if "" == needle {
        return true
    }
    if "" == value {
        return false
    }
    if len(value) < len(needle) {
        return false
    }

    for index := 0; index <= len(value)-len(needle); index++ {
        if value[index:index+len(needle)] == needle {
            return true
        }
    }

    return false
}

func truncateTableCellValue(value string) string {
    maxLength := 220

    if len(value) <= maxLength {
        return value
    }

    return value[:runeAwareByteLimit(value, maxLength-3)] + "..."
}

/* runeAwareByteLimit returns the largest byte offset that is not greater than limit and lands on a UTF-8 rune boundary, so slicing at it never splits a multibyte rune into invalid bytes */
func runeAwareByteLimit(value string, limit int) int {
    if 0 >= limit {
        return 0
    }

    cut := 0

    for cut < len(value) {
        _, size := utf8.DecodeRuneInString(value[cut:])
        if 0 == size {
            break
        }
        if cut+size > limit {
            break
        }

        cut = cut + size
    }

    return cut
}

func truncateTableCellValueByVerbosity(value string, verbosityLevel int) string {
    if 3 <= verbosityLevel {
        return value
    }

    return truncateTableCellValue(value)
}

func buildContainerServiceTableRows(
    item containerServiceListItem,
    option output.Option,
) [][]string {
    typeValue := item.TypeName
    if "" != item.ErrorString {
        typeValue = "<error>"
    }

    if "" == item.ErrorString {
        return [][]string{
            {item.Name, typeValue, ""},
        }
    }

    errorLines := buildContainerServiceErrorLines(item)
    errorLines = limitLinesByVerbosity(errorLines, option.VerbosityLevel)

    rowCount := len(errorLines)
    if 1 > rowCount {
        rowCount = 1
        errorLines = []string{""}
    }

    rows := make([][]string, 0, rowCount)

    for index := 0; index < rowCount; index++ {
        nameCell := ""
        typeCell := ""

        if 0 == index {
            nameCell = item.Name
            typeCell = typeValue
        }

        rows = append(
            rows,
            []string{nameCell, typeCell, errorLines[index]},
        )
    }

    return rows
}

func buildContainerServiceErrorLines(item containerServiceListItem) []string {
    lines := make([]string, 0, 8)

    if "" != item.ErrorString {
        lines = append(lines, splitLines(item.ErrorString)...)
    }

    /* the causes explain the message above them: without these lines the table said a build failed and withheld the dial refusal or missing credential that failed it */
    for _, causeEntry := range item.ErrorCauseChain {
        causeLines := splitLines("caused by: " + causeEntry)
        lines = append(lines, causeLines...)
    }

    if "" != item.ErrorContextJson {
        contextLines := wrapFixedWidth(item.ErrorContextJson, 80)
        for _, contextLine := range contextLines {
            lines = append(lines, contextLine)
        }
    }

    if 0 == len(lines) {
        return []string{""}
    }

    return lines
}

func wrapFixedWidth(value string, width int) []string {
    if "" == value {
        return []string{""}
    }
    if 1 >= width {
        return []string{value}
    }

    result := make([]string, 0, (len(value)/width)+1)

    for 0 < len(value) {
        if len(value) <= width {
            result = append(result, value)
            break
        }

        cut := runeAwareByteLimit(value, width)
        if 0 == cut {
            /* the next rune is wider than the wrap width, so keep it whole rather than splitting its bytes into invalid UTF-8 */
            _, size := utf8.DecodeRuneInString(value)
            cut = size
        }

        result = append(result, value[:cut])
        value = value[cut:]
    }

    return result
}

func splitLines(value string) []string {
    if "" == value {
        return []string{""}
    }

    normalized := strings.ReplaceAll(value, "\r\n", "\n")
    normalized = strings.ReplaceAll(normalized, "\r", "\n")

    lines := strings.Split(normalized, "\n")
    if 0 == len(lines) {
        return []string{""}
    }

    return lines
}

func limitLinesByVerbosity(lines []string, verbosityLevel int) []string {
    maxLines := errorMaxLinesForVerbosityLevel(verbosityLevel)
    if 0 == maxLines {
        return lines
    }

    if len(lines) <= maxLines {
        return lines
    }

    limited := make([]string, 0, maxLines)
    for index := 0; index < maxLines; index++ {
        limited = append(limited, lines[index])
    }

    if 0 < len(limited) {
        limited[len(limited)-1] = limited[len(limited)-1] + " ..."
    }

    return limited
}

func errorMaxLinesForVerbosityLevel(verbosityLevel int) int {
    if 3 <= verbosityLevel {
        return 0
    }
    if 2 == verbosityLevel {
        return 4
    }
    if 1 == verbosityLevel {
        return 2
    }

    return 1
}

func (instance *ContainerCommand) populateSingleService(
    serviceContainer containercontract.Container,
    runScope containercontract.Scope,
    serviceName string,
    option output.Option,
    envelope *output.Envelope,
) {
    scopedNames := map[string]struct{}{}
    if reporter, ok := serviceContainer.(serviceDescriptionReporter); true == ok {
        for _, description := range reporter.ServiceDescriptions() {
            if containercontract.ServiceLifetimeScoped == description.Lifetime {
                scopedNames[description.Name] = struct{}{}
            }
        }
    }

    _, isScoped := scopedNames[serviceName]

    serviceInstance, getErr := resolveServiceForLifetime(serviceContainer, runScope, serviceName, scopedNames)

    typeName := ""
    errorString := ""
    errorContextJson := emptyErrorContextJsonForFormat(option)
    errorCauseChain := []string{}

    if nil != getErr {
        errorString = getErr.Error()
        errorCauseChain = normalizeErrorCauseChain(resolveErrorCauseChain(getErr))
        errorContextJson = resolveErrorContextJson(getErr, option)

        /* a registered service that fails to build is a wiring problem inside the provider, not a missing registration; reporting both as notFound sends the operator after a registration that is in fact present — and a registration either lifetime knows counts as present */
        errorCode := "debug.buildFailed"
        errorMessage := "service failed to build"

        if false == serviceContainer.Has(serviceName) && false == isScoped {
            errorCode = "debug.notFound"
            errorMessage = "service not found"
        }

        causeDetails := (map[string]any)(nil)
        if 0 < len(errorCauseChain) {
            causeDetails = map[string]any{
                "causeChain": errorCauseChain,
            }
        }

        envelope.SetError(
            errorCode,
            errorMessage,
            map[string]any{
                "serviceName": serviceName,
            },
            output.NewErrorCause(
                errorString,
                causeDetails,
            ),
        )
    }

    if nil != serviceInstance {
        typeName = fmt.Sprintf("%T", serviceInstance)
    }

    lifetime := containercontract.ServiceLifetimeContainer
    if true == isScoped {
        lifetime = containercontract.ServiceLifetimeScoped
    }

    details := containerServiceDetails{
        Name:             serviceName,
        Lifetime:         lifetime,
        TypeName:         typeName,
        ErrorString:      errorString,
        ErrorCauseChain:  errorCauseChain,
        ErrorContextJson: errorContextJson,
    }

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()
        builder.AddSummaryLine(
            fmt.Sprintf(
                "SERVICE: %s",
                serviceName,
            ),
        )

        block := builder.AddBlock(
            "DETAILS",
            []string{"key", "value"},
        )

        block.AddRow("name", details.Name)
        block.AddRow("lifetime", details.Lifetime)
        block.AddRow("type", details.TypeName)

        statusValue := "ok"
        if "" != details.ErrorString {
            statusValue = "error"
        }
        block.AddRow("status", statusValue)

        if "" != details.ErrorString {
            block.AddRow("error", details.ErrorString)

            for _, causeEntry := range details.ErrorCauseChain {
                block.AddRow("caused by", causeEntry)
            }

            block.AddRow("errorContextJson", details.ErrorContextJson)
        }

        envelope.Table = builder.Build()

        return
    }

    envelope.Data = details
}

var _ clicontract.Command = (*ContainerCommand)(nil)
