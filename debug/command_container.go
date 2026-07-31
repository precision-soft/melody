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

func (instance *ContainerCommand) Flags() []clicontract.Flag {
    return output.DebugFlags()
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
            serviceName,
            option,
            &envelope,
        )

        envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

        return output.Render(commandContext.Writer, envelope, option)
    }

    instance.populateServiceList(
        serviceContainer,
        option,
        &envelope,
    )

    envelope.Meta.DurationMilliseconds = time.Since(startedAt).Milliseconds()

    return output.Render(commandContext.Writer, envelope, option)
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
    TypeName         string   `json:"typeName"`
    ErrorString      string   `json:"error"`
    ErrorCauseChain  []string `json:"errorCauseChain"`
    ErrorContextJson string   `json:"errorContextJson"`
}

func resolveErrorContextJson(resolveErr error, option output.Option) string {
    if nil == resolveErr {
        return ""
    }

    /* the context is read through the ContextProvider contract rather than the concrete *exception.Error: an HttpException — or any userland error carrying a context — in the resolution chain used to contribute nothing, so its context was silently absent from the one report built to show it */
    var provider exceptioncontract.ContextProvider
    if false == errors.As(resolveErr, &provider) || true == internal.IsNilInterface(provider) {
        return ""
    }

    contextValue := provider.Context()
    if nil == contextValue {
        return ""
    }

    /* @important redact BEFORE marshalling: both fallbacks below print the value they were handed, so sanitizing only the happy path would leak exactly the stack and trace entries this function exists to strip whenever json.Marshal or json.Unmarshal fails */
    /* @important convert the defined exceptioncontract.Context type to its plain map[string]any underlying: sanitizeErrorContextValue matches via value.(map[string]any) first, which fails for the named type; nested named types are converted inside the tracked walk itself */
    redactedContext := sanitizeErrorContextValue(map[string]any(contextValue))

    normalizedContextBytes, normalizeMarshalErr := json.Marshal(redactedContext)
    if nil != normalizeMarshalErr {
        fallbackString := fmt.Sprintf("%v", redactedContext)

        return truncateErrorContextForFormat(fallbackString, option)
    }

    normalizedContext := (any)(nil)
    normalizeUnmarshalErr := json.Unmarshal(normalizedContextBytes, &normalizedContext)
    if nil != normalizeUnmarshalErr {
        fallbackString := fmt.Sprintf("%s", normalizedContextBytes)

        return truncateErrorContextForFormat(fallbackString, option)
    }

    sanitizedContext := sanitizeErrorContextValue(normalizedContext)

    contextJsonBytes, marshalErr := json.Marshal(sanitizedContext)
    if nil != marshalErr {
        fallbackString := fmt.Sprintf("%v", sanitizedContext)

        return truncateErrorContextForFormat(fallbackString, option)
    }

    return truncateErrorContextForFormat(string(contextJsonBytes), option)
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

func (instance *ContainerCommand) populateServiceList(
    serviceContainer containercontract.Container,
    option output.Option,
    envelope *output.Envelope,
) {
    serviceNames := serviceContainer.Names()
    sort.Strings(serviceNames)

    output.ApplySortOrder(serviceNames, option.Order)

    total := len(serviceNames)

    selected := output.WindowItems(serviceNames, option.Limit, option.Offset)

    okItems := make([]containerServiceListItem, 0, len(selected))
    errorItems := make([]containerServiceListItem, 0, len(selected))

    for _, name := range selected {
        serviceInstance, getErr := serviceContainer.Get(name)

        typeName := ""
        errorString := ""
        errorContextJson := ""
        errorCauseChain := ([]string)(nil)

        if nil != getErr {
            errorString = getErr.Error()
            errorCauseChain = resolveErrorCauseChain(getErr)
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
    return sanitizeErrorContextValueTracked(value, map[errorContextVisitKey]struct{}{}, 0)
}

/* the context handed in at the top of resolveErrorContextJson is the caller's own map, redacted before it reaches json.Marshal so the fallbacks cannot print what the redaction exists to strip. That ordering puts this walk ahead of encoding/json's cycle detector, so the walk carries its own: a context holding itself — `context["self"] = context`, which any producer can build — would otherwise recurse until the stack is gone, and a stack overflow is a fatal error that no recover in the command layer turns into a reported failure. */
/* the plain shapes the tracked walk descends into; a defined type sharing their underlying type is converted to them below, which keeps the backing pointer and so the cycle keying */
var plainContextMapType = reflect.TypeOf(map[string]any(nil))
var plainContextSliceType = reflect.TypeOf([]any(nil))

func sanitizeErrorContextValueTracked(value any, seen map[errorContextVisitKey]struct{}, depth int) any {
    if nil == value {
        return nil
    }

    if maximumErrorContextDepth <= depth {
        return errorContextDepthMarker
    }

    mapValue, isMap := value.(map[string]any)
    if false == isMap {
        /* @important a defined type whose underlying type is map[string]any — the framework's own exceptioncontract.Context is one, and it is exactly what a producer reaches for when nesting structured data — fails the assertion above while carrying the same shape. Left unconverted it rode past all three guards at once: a cycle survived into json.Marshal, whose cycle error routed it to the fmt fallback that has no cycle detection of its own — a fatal stack overflow no recover reaches — a depth past the bound recursed inside the encoder, and a redacted key inside it reached the fallbacks in the clear. */
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

        return sanitizeErrorContextMap(mapValue, seen, depth)
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

        return sanitizeErrorContextSlice(sliceValue, seen, depth)
    }

    return value
}

func sanitizeErrorContextMap(value map[string]any, seen map[errorContextVisitKey]struct{}, depth int) map[string]any {
    result := map[string]any{}

    for key, itemValue := range value {
        if true == shouldDropErrorContextKey(key) {
            continue
        }

        result[key] = sanitizeErrorContextValueTracked(itemValue, seen, depth+1)
    }

    return result
}

func sanitizeErrorContextSlice(value []any, seen map[errorContextVisitKey]struct{}, depth int) []any {
    result := make([]any, 0, len(value))

    for _, itemValue := range value {
        result = append(result, sanitizeErrorContextValueTracked(itemValue, seen, depth+1))
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
    serviceName string,
    option output.Option,
    envelope *output.Envelope,
) {
    serviceInstance, getErr := serviceContainer.Get(serviceName)

    typeName := ""
    errorString := ""
    errorContextJson := ""
    errorCauseChain := ([]string)(nil)

    if nil != getErr {
        errorString = getErr.Error()
        errorCauseChain = resolveErrorCauseChain(getErr)
        errorContextJson = resolveErrorContextJson(getErr, option)

        /* a registered service that fails to build is a wiring problem inside the provider, not a missing registration; reporting both as notFound sends the operator after a registration that is in fact present */
        errorCode := "debug.buildFailed"
        errorMessage := "service failed to build"

        if false == serviceContainer.Has(serviceName) {
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

    details := containerServiceDetails{
        Name:             serviceName,
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
