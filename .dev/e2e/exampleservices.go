package main

import (
    "encoding/json"
    "errors"
    "os/exec"
    "path/filepath"
    "sort"
    "strings"
)

/* processServiceCategory names why a process service may hold state, in the vocabulary CONTAINER.md ("Services are stateless by default") declares. A stateless service holds nothing of a request or a call; the other four hold state for a written reason. */
type processServiceCategory string

const (
    processServiceStateless    processServiceCategory = "stateless"
    processServiceStore        processServiceCategory = "cache, limiter or store — the state is the product, under a lock"
    processServiceConnection   processServiceCategory = "connection or pool holder — a resource the teardown closes"
    processServiceBootRegistry processServiceCategory = "boot registry — written while wiring, frozen once serving"
)

/* processServiceClassification is one row of the inventory: the concrete type the service resolved to when its fields were classified, and the category that classification put it in. The type is part of the row on purpose — a service that changes its type has state nobody measured, and the band says so instead of carrying the old verdict forward. */
type processServiceClassification struct {
    typeName string
    category processServiceCategory
}

/* exampleProcessServiceInventory is the v3 example's inventory of process services, classified field by field on 2026-09-18 with a go/types walk over every concrete type (the walk and its output live in the plans repository beside the session that ran it): I 29, L 14, L+C 6, C 12, X 5 over 66 types, the five X being four boot registries and one construction-time builder — no service held request or call state. What the band pins is the FORM of that class: every process service the composition root registers is classified here, by name and by the type it resolves to, so a new service is classified by whoever adds it, a service whose type changed is re-measured, and a row nobody registers any more is removed. The band reads fields of nothing — the measurement was the walk, this is its record. */
var exampleProcessServiceInventory = map[string]processServiceClassification{
    "*github.com/precision-soft/melody/v3/.example/reporting.CatalogReportExporter": {typeName: "*reporting.CatalogReportExporter", category: processServiceStateless},
    "*github.com/precision-soft/melody/v3/.example/reporting.CatalogReportService":  {typeName: "*reporting.CatalogReportService", category: processServiceStateless},
    "*github.com/precision-soft/melody/v3/.example/reporting.ReportFormatter":       {typeName: "*reporting.ReportFormatter", category: processServiceStateless},
    "opentelemetry.otlp.tracer_provider":                                            {typeName: "*otlp.providerHandle", category: processServiceConnection},
    "service-example-archive-database":                                              {typeName: "*bun.DB", category: processServiceConnection},
    "service-example-category-service":                                              {typeName: "*service.CategoryService", category: processServiceStateless},
    "service-example-currency-service":                                              {typeName: "*service.CurrencyService", category: processServiceStateless},
    "service-example-database":                                                      {typeName: "*bun.DB", category: processServiceConnection},
    "service-example-database-registry":                                             {typeName: "*bunorm.ManagerRegistry", category: processServiceConnection},
    "service-example-product-service":                                               {typeName: "*service.ProductService", category: processServiceStateless},
    "service-example-rate-refresh-service":                                          {typeName: "*service.RateRefreshService", category: processServiceStateless},
    "service-example-user-service":                                                  {typeName: "*service.UserService", category: processServiceStateless},
    "service.application.process_role":                                              {typeName: "string", category: processServiceStateless},
    "service.cache":                                                                 {typeName: "*cache.Manager", category: processServiceStateless},
    "service.cache.backend":                                                         {typeName: "*cache.BackendService", category: processServiceStore},
    "service.cache.serializer":                                                      {typeName: "*cache.gobSerializer", category: processServiceStateless},
    "service.clock":                                                                 {typeName: "*clock.SystemClock", category: processServiceStateless},
    "service.config":                                                                {typeName: "*config.Configuration", category: processServiceBootRegistry},
    "service.event.dispatcher":                                                      {typeName: "*event.EventDispatcher", category: processServiceBootRegistry},
    "service.example.archive.locker":                                                {typeName: "*pgsql.Locker", category: processServiceStore},
    "service.example.archive.storage":                                               {typeName: "*persistence.ArchiveStorage", category: processServiceStateless},
    "service.example.catalog.journal.repository":                                    {typeName: "*repository.bunCatalogJournalRepository", category: processServiceStateless},
    "service.example.catalog.journal.service":                                       {typeName: "*service.CatalogJournalService", category: processServiceStateless},
    "service.example.catalog.notification.hub":                                      {typeName: "*http.ServerSentEventHub", category: processServiceStore},
    "service.example.catalog.reading.repository":                                    {typeName: "*repository.bunCatalogReadingRepository", category: processServiceStateless},
    "service.example.catalog.storage":                                               {typeName: "*persistence.CatalogStorage", category: processServiceStateless},
    "service.example.category.repository":                                           {typeName: "*repository.bunCategoryRepository", category: processServiceStateless},
    "service.example.currency.repository":                                           {typeName: "*repository.bunCurrencyRepository", category: processServiceStateless},
    "service.example.product.repository":                                            {typeName: "*repository.bunProductRepository", category: processServiceStateless},
    "service.example.rates.http.client":                                             {typeName: "*httpclient.HttpClient", category: processServiceConnection},
    "service.example.report.export.http.client":                                     {typeName: "*httpclient.HttpClient", category: processServiceConnection},
    "service.example.user.repository":                                               {typeName: "*repository.bunUserRepository", category: processServiceStateless},
    "service.http.route.registry":                                                   {typeName: "*http.RouteRegistry", category: processServiceBootRegistry},
    "service.http.router":                                                           {typeName: "*http.Router", category: processServiceBootRegistry},
    "service.http.url.generator":                                                    {typeName: "*http.UrlGenerator", category: processServiceStateless},
    "service.lock.locker":                                                           {typeName: "*rueidis.Locker", category: processServiceStore},
    "service.logger":                                                                {typeName: "*logging.jsonLogger", category: processServiceConnection},
    "service.mailer.mailer":                                                         {typeName: "*mailer.Manager", category: processServiceStateless},
    "service.messagebus.bus":                                                        {typeName: "*messagebus.Manager", category: processServiceStateless},
    "service.messagebus.consume_bus":                                                {typeName: "*messagebus.Manager", category: processServiceStateless},
    "service.messagebus.transports":                                                 {typeName: "map[string]contract.Transport", category: processServiceConnection},
    "service.messagebus.transports_closer":                                          {typeName: "*messagebus.TransportsCloser", category: processServiceStateless},
    "service.openapi.info":                                                          {typeName: "openapi.Info", category: processServiceStateless},
    "service.openapi.registry":                                                      {typeName: "*openapi.Registry", category: processServiceBootRegistry},
    "service.outbox.relay":                                                          {typeName: "*outbox.Relay", category: processServiceStateless},
    "service.outbox.store":                                                          {typeName: "*outbox.Store", category: processServiceStateless},
    "service.outbox.transport":                                                      {typeName: "*amqp.Transport", category: processServiceConnection},
    "service.rueidis.client":                                                        {typeName: "*rueidis.singleClient", category: processServiceConnection},
    "service.rueidis.connection":                                                    {typeName: "*rueidis.Connection", category: processServiceConnection},
    "service.rueidis.token_store":                                                   {typeName: "*rueidis.RedisTokenStore", category: processServiceStore},
    "service.security.firewall_manager":                                             {typeName: "*security.FirewallManager", category: processServiceStateless},
    "service.serializer":                                                            {typeName: "*serializer.JsonSerializer", category: processServiceStateless},
    "service.serializer.manager":                                                    {typeName: "*serializer.SerializerManager", category: processServiceStateless},
    "service.session.manager":                                                       {typeName: "*session.Manager", category: processServiceStore},
    "service.session.storage":                                                       {typeName: "*session.InMemoryStorage", category: processServiceStore},
    "service.storage.storage":                                                       {typeName: "*awss3.Storage", category: processServiceConnection},
    "service.translation.translator":                                                {typeName: "*translation.Manager", category: processServiceStateless},
    "service.validator":                                                             {typeName: "*validation.Validator", category: processServiceStore},
}

type exampleContainerDescription struct {
    Name     string `json:"name"`
    Lifetime string `json:"lifetime"`
    TypeName string `json:"typeName"`
}

type exampleContainerBuildItem struct {
    Name        string `json:"name"`
    TypeName    string `json:"typeName"`
    ErrorString string `json:"error"`
}

/* runExampleCommandTolerantOfExit runs a command of the example and answers its output whatever the exit status: debug:container --build exits 1 by contract on this example, because the request-scoped report trail cannot be built in a console process (stack.sh asserts exactly that), and the document it wrote before exiting is what this section reads. An exit that is not the process's own verdict — a binary that could not start — still fails. */
func runExampleCommandTolerantOfExit(major exampleMajor, workspace string, arguments ...string) string {
    command := exec.Command(filepath.Join(workspace, "application"), arguments...)
    command.Dir = workspace
    command.Env = exampleRunEnvironment()

    output, runErr := command.CombinedOutput()
    plain := exampleStripAnsi(string(output))

    exitErr := (*exec.ExitError)(nil)
    if nil != runErr && false == errors.As(runErr, &exitErr) {
        fail("[%s] %s could not run: %v:\n%s", major.label, strings.Join(arguments, " "), runErr, exampleTail(plain, 20))
    }

    return plain
}

/* assertExampleProcessServicesAreClassified pins the form of the stateless-by-default class on the live composition root: the describing listing names every registration and its lifetime, the building sweep names the concrete type each process service resolves to, and each of them must be a row of the inventory above, with the type the row was measured on. A row that consumes no registration is dead and fails the same way a missing one does — a list of exceptions that tolerates dead entries grows into a list of excuses. */
func assertExampleProcessServicesAreClassified(major exampleMajor, workspace string) {
    describeOutput := runExampleCommand(major, workspace, "debug:container", "--limit=0", "--format=json")
    describeEnvelope := struct {
        Data struct {
            Items []exampleContainerDescription `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(exampleJsonDocument(describeOutput)), &describeEnvelope); nil != decodeErr {
        fail("[%s] debug:container --format=json emitted no decodable envelope (%v):\n%s", major.label, decodeErr, exampleTail(describeOutput, 20))
    }

    buildOutput := runExampleCommandTolerantOfExit(major, workspace, "debug:container", "--build", "--limit=0", "--format=json")
    buildEnvelope := struct {
        Data struct {
            Items []exampleContainerBuildItem `json:"items"`
        } `json:"data"`
    }{}
    if decodeErr := json.Unmarshal([]byte(exampleJsonDocument(buildOutput)), &buildEnvelope); nil != decodeErr {
        fail("[%s] debug:container --build --format=json emitted no decodable envelope (%v):\n%s", major.label, decodeErr, exampleTail(buildOutput, 20))
    }

    concreteTypeByName := map[string]string{}
    for _, item := range buildEnvelope.Data.Items {
        concreteTypeByName[item.Name] = item.TypeName
    }

    problems, processServices, scopedServices := exampleProcessServiceProblems(describeEnvelope.Data.Items, concreteTypeByName, exampleProcessServiceInventory)

    if 0 == processServices {
        fail("[%s] debug:container listed no process service, so the inventory pin below would be vacuous:\n%s", major.label, exampleTail(describeOutput, 20))
    }

    if 0 < len(problems) {
        fail("[%s] the process-service inventory does not match the composition root (%d problem(s)):\n  %s", major.label, len(problems), strings.Join(problems, "\n  "))
    }

    pass("[%s] every one of the %d process services is classified by name and resolved type (%d scoped, %d rows)", major.label, processServices, scopedServices, len(exampleProcessServiceInventory))
}

/* exampleProcessServiceProblems compares what the composition root registered with the inventory: a process service without a row, a row whose measured type is not the one the service resolves to, and a row no registration consumes are each a problem, named. Scoped services are counted and left alone — their state is the request's by construction. The problems come back sorted, so a failure reads the same on every run. */
func exampleProcessServiceProblems(
    descriptions []exampleContainerDescription,
    concreteTypeByName map[string]string,
    inventory map[string]processServiceClassification,
) ([]string, int, int) {
    processServices := 0
    scopedServices := 0
    consumed := map[string]struct{}{}
    problems := []string{}

    for _, description := range descriptions {
        if "scoped" == description.Lifetime {
            scopedServices++

            continue
        }
        processServices++

        classification, isClassified := inventory[description.Name]
        if false == isClassified {
            problems = append(problems, description.Name+": not classified — a process service holds no request state, or says why on its type; add its row")

            continue
        }
        consumed[description.Name] = struct{}{}

        concreteType, wasBuilt := concreteTypeByName[description.Name]
        if false == wasBuilt || "" == concreteType {
            problems = append(problems, description.Name+": the build sweep resolved no type for it")

            continue
        }
        if classification.typeName != concreteType {
            problems = append(problems, description.Name+": resolves to "+concreteType+" where its state was measured on "+classification.typeName+" — re-measure and update the row")
        }
    }

    for name := range inventory {
        if _, isLive := consumed[name]; false == isLive {
            problems = append(problems, name+": classified but not registered — a dead row is removed, not kept")
        }
    }

    sort.Strings(problems)

    return problems, processServices, scopedServices
}
