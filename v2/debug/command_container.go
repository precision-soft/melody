package debug

import (
    "encoding/json"
    "errors"
    "fmt"
    "sort"
    "strings"
    "time"

    clicontract "github.com/precision-soft/melody/v2/cli/contract"
    "github.com/precision-soft/melody/v2/cli/output"
    containercontract "github.com/precision-soft/melody/v2/container/contract"
    "github.com/precision-soft/melody/v2/exception"
    runtimecontract "github.com/precision-soft/melody/v2/runtime/contract"
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
    Name             string `json:"name"`
    TypeName         string `json:"typeName"`
    ErrorString      string `json:"error"`
    ErrorContextJson string `json:"errorContextJson"`
}

type containerServiceDetails struct {
    Name             string `json:"name"`
    TypeName         string `json:"typeName"`
    ErrorString      string `json:"error"`
    ErrorContextJson string `json:"errorContextJson"`
}

func resolveErrorContextJson(resolveErr error, verbosityLevel int) string {
    if nil == resolveErr {
        return ""
    }

    melodyError := (*exception.Error)(nil)
    if false == errors.As(resolveErr, &melodyError) || nil == melodyError {
        return ""
    }

    contextValue := melodyError.Context()
    if nil == contextValue {
        return ""
    }

    normalizedContextBytes, normalizeMarshalErr := json.Marshal(contextValue)
    if nil != normalizeMarshalErr {
        fallbackString := fmt.Sprintf("%v", contextValue)

        return truncateTableCellValueByVerbosity(fallbackString, verbosityLevel)
    }

    normalizedContext := (any)(nil)
    normalizeUnmarshalErr := json.Unmarshal(normalizedContextBytes, &normalizedContext)
    if nil != normalizeUnmarshalErr {
        fallbackString := fmt.Sprintf("%s", normalizedContextBytes)

        return truncateTableCellValueByVerbosity(fallbackString, verbosityLevel)
    }

    sanitizedContext := sanitizeErrorContextValue(normalizedContext)

    contextJsonBytes, marshalErr := json.Marshal(sanitizedContext)
    if nil != marshalErr {
        fallbackString := fmt.Sprintf("%v", sanitizedContext)

        return truncateTableCellValueByVerbosity(fallbackString, verbosityLevel)
    }

    return truncateTableCellValueByVerbosity(string(contextJsonBytes), verbosityLevel)
}

func (instance *ContainerCommand) populateServiceList(
    serviceContainer containercontract.Container,
    option output.Option,
    envelope *output.Envelope,
) {
    serviceNames := serviceContainer.Names()
    sort.Strings(serviceNames)

    total := len(serviceNames)

    startIndex := option.Offset
    if 0 > startIndex {
        startIndex = 0
    }
    if total < startIndex {
        startIndex = total
    }

    endIndex := total
    if 0 < option.Limit {
        if startIndex+option.Limit < endIndex {
            endIndex = startIndex + option.Limit
        }
    }

    selected := serviceNames[startIndex:endIndex]

    okItems := make([]containerServiceListItem, 0, len(selected))
    errorItems := make([]containerServiceListItem, 0, len(selected))

    for _, name := range selected {
        serviceInstance, getErr := serviceContainer.Get(name)

        typeName := ""
        errorString := ""
        errorContextJson := ""

        if nil != getErr {
            errorString = getErr.Error()
            errorContextJson = resolveErrorContextJson(getErr, option.VerbosityLevel)
        }

        if nil != serviceInstance {
            typeName = fmt.Sprintf("%T", serviceInstance)
        }

        item := containerServiceListItem{
            Name:             name,
            TypeName:         typeName,
            ErrorString:      errorString,
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
            "SERVICES: %d total | %d ok | %d error",
            total,
            len(okItems),
            len(errorItems),
        )

        if shown != total {
            summary = fmt.Sprintf(
                "%s | %d shown",
                summary,
                shown,
            )
        }

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

func sanitizeErrorContextValue(value any) any {
    if nil == value {
        return nil
    }

    mapValue, isMap := value.(map[string]any)
    if true == isMap {
        return sanitizeErrorContextMap(mapValue)
    }

    sliceValue, isSlice := value.([]any)
    if true == isSlice {
        return sanitizeErrorContextSlice(sliceValue)
    }

    return value
}

func sanitizeErrorContextMap(value map[string]any) map[string]any {
    result := map[string]any{}

    for key, itemValue := range value {
        if true == shouldDropErrorContextKey(key) {
            continue
        }

        result[key] = sanitizeErrorContextValue(itemValue)
    }

    return result
}

func sanitizeErrorContextSlice(value []any) []any {
    result := make([]any, 0, len(value))

    for _, itemValue := range value {
        result = append(result, sanitizeErrorContextValue(itemValue))
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

    return value[:maxLength-3] + "..."
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

        result = append(result, value[:width])
        value = value[width:]
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

    if nil != getErr {
        errorString = getErr.Error()
        errorContextJson = resolveErrorContextJson(getErr, option.VerbosityLevel)

        envelope.SetError(
            "debug.notFound",
            "service not found",
            map[string]any{
                "serviceName": serviceName,
            },
            output.NewErrorCause(
                errorString,
                nil,
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
            block.AddRow("errorContextJson", details.ErrorContextJson)
        }

        envelope.Table = builder.Build()

        return
    }

    envelope.Data = details
}

var _ clicontract.Command = (*ContainerCommand)(nil)
