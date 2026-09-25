package debug

import (
    "bytes"
    "encoding"
    "encoding/json"
    "fmt"
    "reflect"
    "slices"
    "sort"
    "strings"
    "time"
    "unicode/utf8"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/cli/output"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
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

/* serviceDescriptionReporter is asked for, not required: a container without it is listed by name alone. */
type serviceDescriptionReporter interface {
    ServiceDescriptions() []containercontract.ServiceDescription
}

func (instance *ContainerCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext clicontract.Context,
) error {
    startedAt := time.Now()

    option := output.NormalizeOption(
        output.ParseOptionFromCommand(commandContext),
    )

    meta := output.NewMeta(
        instance.Name(),
        commandContext.Arguments(),
        option,
        startedAt,
        time.Duration(0),
        output.Version{},
    )

    envelope := output.NewEnvelope(meta)

    serviceContainer := runtimeInstance.Container()

    serviceName := ""
    arguments := commandContext.Arguments()
    if 0 < len(arguments) {
        serviceName = arguments[0]
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

        return output.Render(commandContext.Writer(), envelope, option)
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

    return output.Render(commandContext.Writer(), envelope, option)
}

type containerServiceDescriptionItem struct {
    Name     string                        `json:"name"`
    Lifetime string                        `json:"lifetime"`
    IsBuilt  bool                          `json:"isBuilt"`
    TypeName string                        `json:"typeName"`
    Teardown *containerServiceTeardownItem `json:"teardown,omitempty"`
}

/* describeServiceList is the default listing and runs no provider; a container without the descriptions door is listed by name with a warning. */
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

    view := newTeardownView(serviceContainer)
    shownNames := make(map[string]struct{}, len(items))

    for index := range items {
        items[index].Teardown = view.forService(items[index].Name, items[index].Lifetime)

        if containercontract.ServiceLifetimeContainer == items[index].Lifetime {
            shownNames[items[index].Name] = struct{}{}
        }
    }

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

        view.addBlock(builder, shownNames)

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

/* containerServiceTeardownItem is the teardown's answer about one built container service: its wave, the services it closes before and after, its ordering and the node the plan files it under. It is nil on a service the plan does not list, so the json document omits the key. The ordering reads proved, none, or cycle for a service on a dependency ring the drain closes as one unit. */
type containerServiceTeardownItem struct {
    Wave         int      `json:"wave"`
    Node         string   `json:"node"`
    ClosedBefore []string `json:"closedBefore"`
    ClosedAfter  []string `json:"closedAfter"`
    Ordering     string   `json:"ordering"`
    Group        int      `json:"group"`
}

/* teardownView is the plan read once per command, rendered as the block and carried per service in every form the command answers. A node's ordering is read on both its dependencies and its dependents. A container without the door has no view. */
type teardownView struct {
    entries   []containercontract.TeardownPlanEntry
    byNodeKey map[string]*containerServiceTeardownItem
    aliasesOf map[string][]string
    inWaves   bool
}

/* teardownNameNodeKeyPrefix is the prefix of a node filed under a name, as TeardownPlanEntry documents it. */
const teardownNameNodeKeyPrefix = "service:"

/* teardownTypeNodeKeyPrefix is the prefix of a node filed under a type. */
const teardownTypeNodeKeyPrefix = "type:"

/* newTeardownView answers nil for a container without the door or with nothing built; every reader tolerates the nil. */
func newTeardownView(serviceContainer containercontract.Container) *teardownView {
    planned, carriesPlan := serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
        TeardownRunsInWaves() bool
    })
    if false == carriesPlan {
        return nil
    }

    entries := planned.TeardownPlan()
    if 0 == len(entries) {
        return nil
    }

    byNodeKey := make(map[string]*containerServiceTeardownItem, len(entries))
    aliasesOf := make(map[string][]string, len(entries))

    for _, entry := range entries {
        item := &containerServiceTeardownItem{
            Wave:         entry.WaveIndex,
            Node:         entry.NodeKey,
            ClosedBefore: append([]string{}, entry.Dependencies...),
            ClosedAfter:  []string{},
            Group:        entry.SerialGroup,
        }

        byNodeKey[entry.NodeKey] = item

        /* an alias answers with the item of the node it was collapsed onto */
        for _, alias := range entry.Aliases {
            byNodeKey[alias] = item
        }

        aliasesOf[entry.NodeKey] = entry.Aliases
    }

    /* the plan states each edge at its dependent, so the dependents are derived here */
    for _, entry := range entries {
        for _, dependencyKey := range entry.Dependencies {
            dependency, listed := byNodeKey[dependencyKey]
            if false == listed {
                continue
            }

            dependency.ClosedAfter = append(dependency.ClosedAfter, entry.NodeKey)
        }
    }

    for _, entry := range entries {
        item := byNodeKey[entry.NodeKey]

        sort.Strings(item.ClosedAfter)

        item.Ordering = "proved"
        if 0 == len(item.ClosedBefore) && 0 == len(item.ClosedAfter) {
            item.Ordering = "none"
        }

        if true == entry.Cycle {
            item.Ordering = "cycle"
        }
    }

    return &teardownView{
        entries:   entries,
        byNodeKey: byNodeKey,
        aliasesOf: aliasesOf,
        inWaves:   planned.TeardownRunsInWaves(),
    }
}

/* forService answers the teardown item of a container service filed under its name; nil for a name the plan does not list and for a scoped registration, which may share its name with a built container service but has no node. */
func (instance *teardownView) forService(serviceName string, lifetime string) *containerServiceTeardownItem {
    if nil == instance || containercontract.ServiceLifetimeScoped == lifetime {
        return nil
    }

    return instance.byNodeKey[teardownNameNodeKeyPrefix+serviceName]
}

/* readableAliases renders an alias node key readably: a type key is shown as the type's string, not the raw package path and NUL. */
func readableAliases(aliases []string) []string {
    readable := make([]string, 0, len(aliases))

    for _, alias := range aliases {
        if true == strings.HasPrefix(alias, teardownTypeNodeKeyPrefix) {
            if separator := strings.LastIndex(alias, "\x00"); 0 <= separator {
                alias = teardownTypeNodeKeyPrefix + alias[separator+1:]
            }
        }

        readable = append(readable, alias)
    }

    return readable
}

/* addBlock renders the plan for the nodes the listing shows: a name node when any of its names is in the window, a type-only node always. The wave index is the plan's, not the window's. The window holds the container services shown, never a scoped registration. */
func (instance *teardownView) addBlock(builder *output.TableBuilder, shownNames map[string]struct{}) {
    if nil == instance {
        return
    }

    rows := make([][]string, 0, len(instance.entries))

    for _, entry := range instance.entries {
        if true == strings.HasPrefix(entry.NodeKey, teardownNameNodeKeyPrefix) && false == instance.isShown(entry, shownNames) {
            continue
        }

        item := instance.byNodeKey[entry.NodeKey]

        node := entry.NodeKey
        if 0 < len(entry.Aliases) {
            node = fmt.Sprintf("%s (also %s)", entry.NodeKey, strings.Join(readableAliases(entry.Aliases), ", "))
        }

        /* a group is printed by its number, empty for a service in none: two rows sharing it close one after the other */
        group := ""
        if 0 != item.Group {
            group = fmt.Sprintf("%d", item.Group)
        }

        rows = append(
            rows,
            []string{
                fmt.Sprintf("%d", entry.WaveIndex),
                node,
                strings.Join(item.ClosedBefore, ", "),
                item.Ordering,
                strings.Join(item.ClosedAfter, ", "),
                group,
            },
        )
    }

    if 0 == len(rows) {
        return
    }

    title := "TEARDOWN (SEQUENTIAL)"
    if true == instance.inWaves {
        title = "TEARDOWN (DEPENDENCY WAVES)"
    }

    teardownBlock := builder.AddBlock(
        title,
        []string{"wave", "node", "closed before", "ordering", "closed after", "group"},
    )

    for _, row := range rows {
        teardownBlock.AddRow(row...)
    }
}

/* isShown answers whether a name node is in the window under any of its names. */
func (instance *teardownView) isShown(entry containercontract.TeardownPlanEntry, shownNames map[string]struct{}) bool {
    if _, shown := shownNames[strings.TrimPrefix(entry.NodeKey, teardownNameNodeKeyPrefix)]; true == shown {
        return true
    }

    for _, alias := range entry.Aliases {
        if false == strings.HasPrefix(alias, teardownNameNodeKeyPrefix) {
            continue
        }

        if _, shown := shownNames[strings.TrimPrefix(alias, teardownNameNodeKeyPrefix)]; true == shown {
            return true
        }
    }

    return false
}

func renderServiceDescriptionState(item containerServiceDescriptionItem) string {
    if true == item.IsBuilt {
        return "built"
    }

    return "registered"
}

type containerServiceListItem struct {
    Name             string                        `json:"name"`
    Lifetime         string                        `json:"lifetime"`
    TypeName         string                        `json:"typeName"`
    ErrorString      string                        `json:"error"`
    ErrorCauseChain  []string                      `json:"errorCauseChain"`
    ErrorContextJson string                        `json:"errorContextJson"`
    Teardown         *containerServiceTeardownItem `json:"teardown,omitempty"`
}

type containerServiceDetails struct {
    Name             string                        `json:"name"`
    Lifetime         string                        `json:"lifetime"`
    TypeName         string                        `json:"typeName"`
    ErrorString      string                        `json:"error"`
    ErrorCauseChain  []string                      `json:"errorCauseChain"`
    ErrorContextJson string                        `json:"errorContextJson"`
    Teardown         *containerServiceTeardownItem `json:"teardown,omitempty"`
}

/* errorCauseChainDepth bounds the links the report walks, for the cause chain and its context alike. */
const errorCauseChainDepth = 9

func resolveErrorContextJson(resolveErr error, option output.Option) string {
    if nil == resolveErr {
        return emptyErrorContextJsonForFormat(option)
    }

    /* the context is read through the ContextProvider contract from the first link that has one, breadth-first over the chain and bounded like it: a provider without a context answers an empty map */
    contextValue := firstErrorContextInChain(resolveErr)
    if nil == contextValue {
        return emptyErrorContextJsonForFormat(option)
    }

    /* sanitized before marshalling, because both fallbacks below print the value they were handed */
    keepNoiseKeys := 3 <= option.VerbosityLevel
    redactedContext := sanitizeErrorContextValueTracked(contextValue, map[errorContextVisitKey]struct{}{}, 0, keepNoiseKeys)

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

/* firstErrorContextInChain answers the context of the first link that carries one, within the cause chain's depth bound, and nil when none does. */
func firstErrorContextInChain(resolveErr error) map[string]any {
    for _, linkContext := range exception.BuildCauseContextChain(resolveErr, errorCauseChainDepth) {
        if 0 < len(linkContext) {
            return linkContext
        }
    }

    return nil
}

/* emptyErrorContextJsonForFormat answers the absence of a context in the format's grammar: "{}" in json, so the field always parses, and an empty cell in the table. */
func emptyErrorContextJsonForFormat(option output.Option) string {
    if output.FormatTable == option.Format {
        return ""
    }

    return "{}"
}

/* unrepresentableErrorContextForFormat answers a context json.Marshal refuses in the format's grammar: in json an object carrying the rendering under a key, so the field always parses; in the table the bare rendering. */
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

/* normalizeErrorCauseChain answers an empty list rather than nil, so the field is an array on every row. */
func normalizeErrorCauseChain(causeChain []string) []string {
    if nil == causeChain {
        return []string{}
    }

    return causeChain
}

/* truncateErrorContextForFormat truncates the table cell only: a cut json fragment would not parse. */
func truncateErrorContextForFormat(value string, option output.Option) string {
    if output.FormatTable != option.Format {
        return value
    }

    return truncateTableCellValueByVerbosity(value, option.VerbosityLevel)
}

/* resolveErrorCauseChain walks the causes below the resolution error's own message, so the report names why the build failed. */
func resolveErrorCauseChain(resolveErr error) []string {
    if nil == resolveErr {
        return nil
    }

    /* built from the failure with the head dropped, since BuildCauseChain walks both unwrap shapes */
    chain := exception.BuildCauseChain(resolveErr, errorCauseChainDepth)
    if 1 >= len(chain) {
        return nil
    }

    return chain[1:]
}

/* sweepRegistration is one registration the --build sweep resolves, with the lifetime it was registered under: a scoped registration sharing a name with a container service is a registration of its own. */
type sweepRegistration struct {
    name     string
    lifetime string
}

/* populateServiceList is the --build sweep: every windowed service is resolved and each failure reports its causes. A scoped registration resolves through the run's own scope. */
func (instance *ContainerCommand) populateServiceList(
    serviceContainer containercontract.Container,
    runScope containercontract.Scope,
    option output.Option,
    envelope *output.Envelope,
) {
    registrations := ([]sweepRegistration)(nil)

    if reporter, ok := serviceContainer.(serviceDescriptionReporter); true == ok {
        descriptions := reporter.ServiceDescriptions()

        registrations = make([]sweepRegistration, 0, len(descriptions))
        for _, description := range descriptions {
            registrations = append(registrations, sweepRegistration{name: description.Name, lifetime: description.Lifetime})
        }
    } else {
        for _, name := range serviceContainer.Names() {
            registrations = append(registrations, sweepRegistration{name: name, lifetime: containercontract.ServiceLifetimeContainer})
        }
    }

    /* sorted stably, so two registrations of one name keep the description's order */
    slices.SortStableFunc(registrations, func(left sweepRegistration, right sweepRegistration) int {
        return strings.Compare(left.name, right.name)
    })

    output.ApplySortOrder(registrations, option.Order)

    total := len(registrations)

    selected := output.WindowItems(registrations, option.Limit, option.Offset)

    okItems := make([]containerServiceListItem, 0, len(selected))
    errorItems := make([]containerServiceListItem, 0, len(selected))

    /* read after the sweep, since the plan lists what is built */
    shownNames := make(map[string]struct{}, len(selected))

    for _, registration := range selected {
        name := registration.name

        if containercontract.ServiceLifetimeContainer == registration.lifetime {
            shownNames[name] = struct{}{}
        }

        serviceInstance, getErr := resolveServiceForLifetime(serviceContainer, runScope, name, registration.lifetime)

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
            Lifetime:         registration.lifetime,
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

    view := newTeardownView(serviceContainer)

    for index := range okItems {
        okItems[index].Teardown = view.forService(okItems[index].Name, okItems[index].Lifetime)
    }

    for index := range errorItems {
        errorItems[index].Teardown = view.forService(errorItems[index].Name, errorItems[index].Lifetime)
    }

    reportServiceSweepFailures(errorItems, envelope)

    if output.FormatTable == option.Format {
        builder := output.NewTableBuilder()
        shown := len(okItems) + len(errorItems)

        summary := fmt.Sprintf(
            "SERVICES: %d total",
            total,
        )

        /* the shown count precedes the ok/error split, which covers the windowed services only */
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

                    errorLines := buildContainerServiceErrorLines(item, option.VerbosityLevel)

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

        view.addBlock(builder, shownNames)

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

/* reportServiceSweepFailures carries the sweep's failures on the envelope, so the command exits non-zero over them and can gate a deployment. */
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

/* resolveServiceForLifetime resolves through the owner of the registration: the container, or the run's scope for a scoped one. */
func resolveServiceForLifetime(
    serviceContainer containercontract.Container,
    runScope containercontract.Scope,
    serviceName string,
    lifetime string,
) (any, error) {
    if containercontract.ServiceLifetimeScoped == lifetime && false == internal.IsNilInterface(runScope) {
        return runScope.Get(serviceName)
    }

    return serviceContainer.Get(serviceName)
}

/* errorContextCycleMarker stands in for a container the walk meets on its own path. */
const errorContextCycleMarker = "<cycle>"

/* errorContextDepthMarker stands in for a subtree past maximumErrorContextDepth. */
const errorContextDepthMarker = "<depth limit>"

/* errorContextMarshalFailureMarker stands in for a self-rendering value whose rendering failed; none of it is shown. */
const errorContextMarshalFailureMarker = "<marshal failed>"

/* maximumErrorContextDepth bounds the descent, since a deep enough acyclic context would overflow the stack, a fatal error no recover reaches. It matches the bound internal/copy.go puts on the same walk. */
const maximumErrorContextDepth = 10000

/* errorContextVisitKey keys the containers on the current path only: the same map under two sibling keys is not a cycle. A slice is keyed on its backing pointer and length. */
type errorContextVisitKey struct {
    pointer uintptr
    length  uintptr
}

/* the plain shapes the walk descends into; a defined type over them is converted, which keeps the backing pointer and so the cycle keying */
var plainContextMapType = reflect.TypeOf(map[string]any(nil))
var plainContextSliceType = reflect.TypeOf([]any(nil))

func sanitizeErrorContextValueTracked(value any, seen map[errorContextVisitKey]struct{}, depth int, keepNoiseKeys bool) any {
    if nil == value {
        return nil
    }

    if maximumErrorContextDepth <= depth {
        return errorContextDepthMarker
    }

    /* a map or slice type that renders itself is rendered through its own method and the result walked, since the conversion below strips that method */
    if rendered, isRendered := renderedThroughItsOwnJson(value); true == isRendered {
        value = rendered
    }

    mapValue, isMap := value.(map[string]any)
    if false == isMap {
        /* a defined type over map[string]any, such as exceptioncontract.Context, is converted so the cycle, depth and noise guards apply to it */
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
        /* a defined type over []any is converted for the same reason */
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

/* renderedThroughItsOwnJson renders a self-marshaling map or slice through its own method and decodes the result into the plain shapes, numbers kept as written. A failing or panicking method renders as the failure marker, never as the plain shape its method masks. */
func renderedThroughItsOwnJson(value any) (rendered any, isRendered bool) {
    /* only the shapes the walk would otherwise convert; any other self-marshaling value reaches the encoder as it is */
    if kind := reflect.ValueOf(value).Kind(); reflect.Map != kind && reflect.Slice != kind {
        return nil, false
    }

    _, isMarshaler := value.(json.Marshaler)
    _, isTextMarshaler := value.(encoding.TextMarshaler)
    if false == isMarshaler && false == isTextMarshaler {
        return nil, false
    }

    defer func() {
        if nil != recover() {
            rendered = errorContextMarshalFailureMarker
            isRendered = true
        }
    }()

    encoded, marshalErr := json.Marshal(value)
    if nil != marshalErr {
        return errorContextMarshalFailureMarker, true
    }

    decoder := json.NewDecoder(bytes.NewReader(encoded))
    decoder.UseNumber()
    if decodeErr := decoder.Decode(&rendered); nil != decodeErr {
        return errorContextMarshalFailureMarker, true
    }

    return rendered, true
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

/* shouldDropErrorContextKey names diagnostic noise, not secrets: stack and trace entries are dropped below full verbosity. Nothing else is redacted. */
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

/* runeAwareByteLimit returns the largest byte offset not above limit that lands on a UTF-8 rune boundary. */
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

    errorLines := buildContainerServiceErrorLines(item, option.VerbosityLevel)

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

/* buildContainerServiceErrorLines renders one failed service's error cell: the verbosity ladder cuts the message and the context json, never the causes. */
func buildContainerServiceErrorLines(item containerServiceListItem, verbosityLevel int) []string {
    messageLines := []string{}
    if "" != item.ErrorString {
        messageLines = splitLines(item.ErrorString)
    }

    causeLines := make([]string, 0, len(item.ErrorCauseChain))
    for _, causeEntry := range item.ErrorCauseChain {
        causeLines = append(causeLines, splitLines("caused by: "+causeEntry)...)
    }

    contextLines := []string{}
    if "" != item.ErrorContextJson {
        contextLines = wrapFixedWidth(item.ErrorContextJson, 80)
    }

    lines := limitErrorLinesByVerbosity(messageLines, causeLines, contextLines, verbosityLevel)

    if 0 == len(lines) {
        return []string{""}
    }

    return lines
}

/* limitErrorLinesByVerbosity applies the ladder to the message and the context as one budget and splices the cause lines whole between them; the cut marker stays on the last rendered line. */
func limitErrorLinesByVerbosity(messageLines []string, causeLines []string, contextLines []string, verbosityLevel int) []string {
    budgeted := make([]string, 0, len(messageLines)+len(contextLines))
    budgeted = append(budgeted, messageLines...)
    budgeted = append(budgeted, contextLines...)

    limited := limitLinesByVerbosity(budgeted, verbosityLevel)
    cut := len(limited) < len(budgeted)
    if true == cut && 0 < len(limited) {
        limited[len(limited)-1] = strings.TrimSuffix(limited[len(limited)-1], verbosityCutMarker)
    }

    messageShown := len(messageLines)
    if messageShown > len(limited) {
        messageShown = len(limited)
    }

    lines := make([]string, 0, len(limited)+len(causeLines))
    lines = append(lines, limited[:messageShown]...)
    lines = append(lines, causeLines...)
    lines = append(lines, limited[messageShown:]...)

    if true == cut && 0 < len(lines) {
        lines[len(lines)-1] = lines[len(lines)-1] + verbosityCutMarker
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
            /* a rune wider than the wrap width is kept whole */
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
        limited[len(limited)-1] = limited[len(limited)-1] + verbosityCutMarker
    }

    return limited
}

/* verbosityCutMarker is the suffix of a line the ladder left lines out below. */
const verbosityCutMarker = " ..."

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
    /* a name a scoped registration answers to resolves as scoped, even where a container service shares it */
    lifetime := containercontract.ServiceLifetimeContainer
    if reporter, ok := serviceContainer.(serviceDescriptionReporter); true == ok {
        for _, description := range reporter.ServiceDescriptions() {
            if serviceName == description.Name && containercontract.ServiceLifetimeScoped == description.Lifetime {
                lifetime = containercontract.ServiceLifetimeScoped
            }
        }
    }

    isScoped := containercontract.ServiceLifetimeScoped == lifetime

    serviceInstance, getErr := resolveServiceForLifetime(serviceContainer, runScope, serviceName, lifetime)

    typeName := ""
    errorString := ""
    errorContextJson := emptyErrorContextJsonForFormat(option)
    errorCauseChain := []string{}

    if nil != getErr {
        errorString = getErr.Error()
        errorCauseChain = normalizeErrorCauseChain(resolveErrorCauseChain(getErr))
        errorContextJson = resolveErrorContextJson(getErr, option)

        /* a registered service that fails to build is a wiring error, not notFound; a registration of either lifetime counts as present */
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

    view := newTeardownView(serviceContainer)

    details := containerServiceDetails{
        Name:             serviceName,
        Lifetime:         lifetime,
        TypeName:         typeName,
        ErrorString:      errorString,
        ErrorCauseChain:  errorCauseChain,
        ErrorContextJson: errorContextJson,
        Teardown:         view.forService(serviceName, lifetime),
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

        /* windowed on this service only where the plan can list it */
        shownNames := map[string]struct{}{}
        if false == isScoped {
            shownNames[serviceName] = struct{}{}
        }

        view.addBlock(builder, shownNames)

        envelope.Table = builder.Build()

        return
    }

    envelope.Data = details
}

var _ clicontract.Command = (*ContainerCommand)(nil)
