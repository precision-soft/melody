package main

import (
    "encoding/json"
    "fmt"
    "io"
    "net"
    "net/http"
    "net/http/cookiejar"
    "net/url"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
    "time"
)

/* exampleMajorListVariable names the environment variable that selects which majors the per-major example sections exercise. Unset means every major — the project's default run is the full run. */
const exampleMajorListVariable = "MELODY_E2E_MAJORS"

const exampleMajorListDefault = "1 2 3"

/* the shared credentials the three example applications seed identically (repository/seed.go): "user" holds ROLE_USER only, which is what makes the admin route below answer 403 rather than 200. */
const (
    exampleUsername       = "user"
    examplePassword       = "user"
    exampleWrongPassword  = "definitely-not-the-password"
    exampleSessionCookie  = "MELODYSESSID"
    exampleReadinessRoute = "/routes/"
)

/* exampleMajor names one major's .example application: where it lives in the tree and which port the harness serves its copy on. The ports are distinct so all three can run at once, and none of them is 8080 (the dev-supervised v3 example) or 18080 (stack.sh's signal-shutdown check). */
type exampleMajor struct {
    number            int
    label             string
    relativeDirectory string
    port              int
    /* integrationDemos is off for v3 on purpose. Its example drives the same rate-limit counter that EXAMPLE OVER HTTP measures an exact exhaustion point on against the supervised application, so running the demos here as well would spend a budget another section is counting. */
    integrationDemos bool
    /* showcaseProbes and sessionRestartProbe gate the wirings the two published examples carry — cors, gzip, the api-key firewall, per-field validation errors, the trusted-proxy client address, the file-backed session storage and the static cache validators. The three examples deliberately do not mirror each other, so these are per-major capabilities like integrationDemos, not shared surface. */
    showcaseProbes      bool
    sessionRestartProbe bool
    /* loginThrottleProbe names the majors whose example puts the login submit behind the shared per-address write budget. It is a per-major capability like the two above: v3's example declares the route public and leaves it unthrottled, so asserting the refusal there would assert a wiring it does not carry. */
    loginThrottleProbe bool
    /* journalOnPostgres names where the major keeps its catalog journal: the v1 example runs two live databases in one process — the catalogue on mysql, the journal on postgres — so its out-of-band journal reads go through the postgres door while the later majors keep reading mysql. */
    journalOnPostgres bool
}

var exampleMajorCatalog = []exampleMajor{
    {number: 1, label: "v1", relativeDirectory: ".example", port: 18081, integrationDemos: true, showcaseProbes: true, sessionRestartProbe: true, loginThrottleProbe: true, journalOnPostgres: true},
    {number: 2, label: "v2", relativeDirectory: "v2/.example", port: 18082, integrationDemos: true, showcaseProbes: true, sessionRestartProbe: true, loginThrottleProbe: true},
    {number: 3, label: "v3", relativeDirectory: "v3/.example", port: 18083, integrationDemos: false},
}

/* exampleMysqlDsn answers the dsn of one major's own database. The three examples share the development mysql but not a database in it — each holds its schema in melody_example_v<major> — so a harness section that reads what an application wrote has to ask the database that application writes to. MYSQL_DSN carries v3's, the one the supervised sections use, and this swaps the database segment of it for the major being driven.

   An empty dsn stays empty: it is how a caller says there is no database to reach, and the sections that receive it already announce their out-of-band half as skipped. A dsn with no database segment is answered unchanged rather than repaired — the caller configured it, and silently pointing it somewhere else would hide that. */
func exampleMysqlDsn(major exampleMajor, mysqlDsn string) string {
    if "" == mysqlDsn {
        return ""
    }

    separatorIndex := strings.LastIndex(mysqlDsn, "/")
    if 0 > separatorIndex {
        return mysqlDsn
    }

    parameters := ""
    remainder := mysqlDsn[separatorIndex+1:]
    if parameterIndex := strings.Index(remainder, "?"); 0 <= parameterIndex {
        parameters = remainder[parameterIndex:]
    }

    return mysqlDsn[:separatorIndex+1] + exampleMysqlDatabase(major) + parameters
}

/* exampleMysqlDatabase names one major's database. The spelling is the one the .env of that example carries and the one .dev/docker/mysql/init.sql creates. */
func exampleMysqlDatabase(major exampleMajor) string {
    return "melody_example_v" + strconv.Itoa(major.number)
}

/* exampleMajorList resolves the majors to exercise from MELODY_E2E_MAJORS, which accepts space or comma separated major numbers ("1 2 3", "1,3", "v2"). An UNSET variable means all three, so a default run drives every major; an empty value opts out of the per-major sections the same way clearing a backend variable opts out of the sections that backend gates. An unknown entry is a hard error rather than a silent narrowing of coverage. */
func exampleMajorList() []exampleMajor {
    raw, isSet := os.LookupEnv(exampleMajorListVariable)
    if false == isSet {
        raw = exampleMajorListDefault
    }

    entries := strings.FieldsFunc(raw, func(candidate rune) bool {
        return ' ' == candidate || ',' == candidate || '\t' == candidate || '\n' == candidate
    })

    majors := make([]exampleMajor, 0, len(entries))

    for _, entry := range entries {
        number, convertErr := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(entry), "v"))
        if nil != convertErr {
            fail("%s: %q is not a melody major number", exampleMajorListVariable, entry)
        }

        major, exists := exampleMajorByNumber(number)
        if false == exists {
            fail("%s: melody has no major %d — expected one of 1, 2, 3", exampleMajorListVariable, number)
        }

        majors = append(majors, major)
    }

    return majors
}

func exampleMajorByNumber(number int) (exampleMajor, bool) {
    for _, major := range exampleMajorCatalog {
        if number == major.number {
            return major, true
        }
    }

    return exampleMajor{}, false
}

/* exampleRepositoryDirectory locates the repository root from the harness's own working directory (.dev/e2e), so the sections address the example applications by tree position instead of hardcoding the container path. */
func exampleRepositoryDirectory() string {
    workingDirectory, workingDirectoryErr := os.Getwd()
    if nil != workingDirectoryErr {
        fail("example application: resolve the working directory: %v", workingDirectoryErr)
    }

    return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}

func (instance exampleMajor) directory() string {
    return filepath.Join(exampleRepositoryDirectory(), instance.relativeDirectory)
}

func (instance exampleMajor) baseUrl() string {
    return fmt.Sprintf("http://127.0.0.1:%d", instance.port)
}

/* runExampleApplicationCheck builds one major's .example application, boots it, drives it over real HTTP with a
cookie jar, runs its command-line surface and shuts it down with a single SIGINT.

The application is built into a workspace under the temporary directory rather than into the example itself,
which is what keeps the coverage repeatable:

  - melody derives the project directory from the location of the EXECUTABLE, so the binary sits beside the
    .env it must read and the port override goes into a .env.local in that workspace. Nothing is written into
    the example's own directory, so no built binary and no .env.local can be left behind there;
  - the workspace sits outside the repository bind mount, so the file watcher that supervises the v3 example on
    :8080 never sees a change and never restarts it mid-check.

The binary is built UNTAGGED on purpose: under melody_env_embedded the env files are baked in at build time from
the example directory, so a .env.local written into the workspace afterwards would be ignored and the
application would fight the supervised one for :8080. */
func runExampleApplicationCheck(major exampleMajor, redisAddress string, mysqlDsn string, postgresDsn string) {
    /* swapped here rather than at each reader: everything below this line is about ONE major, and the dsn the caller passes names v3's database whatever major is being driven */
    mysqlDsn = exampleMysqlDsn(major, mysqlDsn)

    workspace := filepath.Join(os.TempDir(), "melody-e2e-example-"+major.label)

    prepareExampleWorkspace(major, workspace)
    removeWorkspaceOnFailure := pushFailureCleanup(func() {
        _ = os.RemoveAll(workspace)
    })
    defer func() {
        removeWorkspaceOnFailure()
        _ = os.RemoveAll(workspace)
    }()

    requireFreeExamplePort(major)

    application := startExampleApplication(major, workspace)
    stopApplicationOnFailure := pushFailureCleanup(application.kill)

    waitForExampleReadiness(major, application)
    pass("[%s] built from %s and answered %s on port %d", major.label, major.relativeDirectory, exampleReadinessRoute, major.port)

    runExampleHttpAssertions(major, application, redisAddress, mysqlDsn, postgresDsn)
    runExampleCliAssertions(major, workspace)

    if true == major.sessionRestartProbe {
        application = assertExampleSessionSurvivesRestart(major, workspace, application, &stopApplicationOnFailure)
    }

    assertExampleGracefulShutdown(major, application)

    stopApplicationOnFailure()
}

/* assertExampleSessionSurvivesRestart proves the file-backed session storage the v1 example configures through APP_SESSION_FILE: a fresh client signs in, the process is stopped through the same graceful-shutdown assertion the section ends with, a NEW process is started over the same workspace, and the same cookie jar must still admit the protected route — which can only hold if the session outlived the process on disk. The caller's failure-cleanup slot is swapped to the new process, and the new handle is returned so the closing shutdown assertion drives the process that is actually serving. */
func assertExampleSessionSurvivesRestart(
    major exampleMajor,
    workspace string,
    application *exampleApplication,
    stopApplicationOnFailure *func(),
) *exampleApplication {
    client := newExampleClient(major)

    signedIn := client.call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody(exampleUsername, examplePassword))
    if http.StatusOK != signedIn.statusCode {
        fail("[%s] the restart probe could not sign in (%d): %s", major.label, signedIn.statusCode, exampleTruncate(signedIn.body))
    }
    if nil == client.sessionCookie() {
        fail("[%s] the restart probe holds no session cookie after login", major.label)
    }

    granted := client.call("GET", exampleUserRoute, "", "", "")
    if http.StatusOK != granted.statusCode {
        fail("[%s] %s answered %d to the fresh session before the restart, wanted 200", major.label, exampleUserRoute, granted.statusCode)
    }

    assertExampleGracefulShutdown(major, application)
    (*stopApplicationOnFailure)()

    restarted := startExampleApplication(major, workspace)
    *stopApplicationOnFailure = pushFailureCleanup(restarted.kill)

    waitForExampleReadiness(major, restarted)

    replayed := client.call("GET", exampleUserRoute, "", "", "")
    if http.StatusOK != replayed.statusCode {
        fail(
            "[%s] %s answered %d to the pre-restart session cookie, wanted 200 — the session did not survive the process (file-backed storage)\n%s",
            major.label,
            exampleUserRoute,
            replayed.statusCode,
            restarted.logTail(20),
        )
    }
    pass("[%s] the session cookie issued before the restart still admits after it (file-backed session storage)", major.label)

    return restarted
}

/* prepareExampleWorkspace assembles the runnable copy: the built binary, the example's own .env, its public directory (the static surface the traversal probes need) and the .env.local that moves the http address off the port the dev container already serves. */
func prepareExampleWorkspace(major exampleMajor, workspace string) {
    if removeErr := os.RemoveAll(workspace); nil != removeErr {
        fail("[%s] example application: clear the workspace %s: %v", major.label, workspace, removeErr)
    }
    if makeErr := os.MkdirAll(workspace, 0o755); nil != makeErr {
        fail("[%s] example application: create the workspace %s: %v", major.label, workspace, makeErr)
    }

    directory := major.directory()
    if _, statErr := os.Stat(filepath.Join(directory, "go.mod")); nil != statErr {
        fail("[%s] example application: %s is not a go module: %v", major.label, directory, statErr)
    }

    build := exec.Command("go", "build", "-o", filepath.Join(workspace, "application"), ".")
    build.Dir = directory
    build.Env = exampleBuildEnvironment()

    if buildOutput, buildErr := build.CombinedOutput(); nil != buildErr {
        fail("[%s] example application: build %s: %v\n%s", major.label, directory, buildErr, buildOutput)
    }

    environmentFile, readErr := os.ReadFile(filepath.Join(directory, ".env"))
    if nil != readErr {
        fail("[%s] example application: read %s/.env: %v", major.label, directory, readErr)
    }
    if writeErr := os.WriteFile(filepath.Join(workspace, ".env"), environmentFile, 0o644); nil != writeErr {
        fail("[%s] example application: write the workspace .env: %v", major.label, writeErr)
    }

    /* nothing a browser needs beyond the HTML is committed: the bundle is emitted from assets/app.ts and the four icons are copied out of <root>/.assets, both into a git-ignored public/, by the one `npm run build` the container entrypoint runs per major at startup. Their absence is caught HERE, where the cause can be named, instead of downstream as bare 404s that read like a routing or static-surface fault. */
    if _, statErr := os.Stat(filepath.Join(directory, "assets", "package.json")); nil == statErr {
        producedPathList := []string{
            filepath.Join("public", "assets", "app.js"),
            filepath.Join("public", "favicon.ico"),
            filepath.Join("public", "assets", "favicon.svg"),
            filepath.Join("public", "assets", "logo.png"),
            filepath.Join("public", "assets", "apple-touch-icon.png"),
        }

        for _, producedPath := range producedPathList {
            if _, producedErr := os.Stat(filepath.Join(directory, producedPath)); nil != producedErr {
                fail("[%s] example application: %s has a frontend source in assets/ but %s was never produced — run `cd %s/assets && npm ci && npm run build` (the container entrypoint does this per major at startup): %v", major.label, major.relativeDirectory, producedPath, major.relativeDirectory, producedErr)
            }
        }
    }

    if copyErr := os.CopyFS(filepath.Join(workspace, "public"), os.DirFS(filepath.Join(directory, "public"))); nil != copyErr {
        fail("[%s] example application: copy the public directory: %v", major.label, copyErr)
    }

    override := fmt.Sprintf("MELODY_HTTP_ADDRESS=:%d\n", major.port)
    if writeErr := os.WriteFile(filepath.Join(workspace, ".env.local"), []byte(override), 0o644); nil != writeErr {
        fail("[%s] example application: write the workspace .env.local: %v", major.label, writeErr)
    }
}

/* exampleBuildEnvironment keeps the module resolution the harness itself runs under (GOWORK=off plus go on the path), since every example resolves the framework through its own replace directives. */
func exampleBuildEnvironment() []string {
    environment := []string{
        "PATH=" + os.Getenv("PATH"),
        "HOME=" + os.Getenv("HOME"),
        "GOWORK=off",
    }

    if cache := os.Getenv("GOCACHE"); "" != cache {
        environment = append(environment, "GOCACHE="+cache)
    }
    if modules := os.Getenv("GOMODCACHE"); "" != modules {
        environment = append(environment, "GOMODCACHE="+modules)
    }
    if flags := os.Getenv("GOFLAGS"); "" != flags {
        environment = append(environment, "GOFLAGS="+flags)
    }

    return environment
}

/* exampleRunEnvironment deliberately withholds the harness's backend variables from the application: melody resolves configuration from the .env files beside the executable, so the application under test runs configured exactly as it ships. Clearing REDIS_ADDRESS to skip the redis-backed sections therefore cannot reach in and reconfigure the example too. */
func exampleRunEnvironment() []string {
    return []string{
        "PATH=" + os.Getenv("PATH"),
        "HOME=" + os.Getenv("HOME"),
    }
}

/* exampleApplication is a started example process together with the channel its exit status arrives on; Wait is called exactly once, in the goroutine, so both the readiness loop and the shutdown assertion can observe it. */
type exampleApplication struct {
    major    exampleMajor
    command  *exec.Cmd
    exitList chan error
    logPath  string
}

func (instance *exampleApplication) kill() {
    if nil == instance.command || nil == instance.command.Process {
        return
    }

    _ = instance.command.Process.Kill()
}

/* logTail returns the last lines the application wrote, for the diagnostics that must say WHY the section could not run rather than blaming the property under test. */
func (instance *exampleApplication) logTail(lines int) string {
    content, readErr := os.ReadFile(instance.logPath)
    if nil != readErr {
        return fmt.Sprintf("(the application log at %s could not be read: %v)", instance.logPath, readErr)
    }

    all := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
    if len(all) > lines {
        all = all[len(all)-lines:]
    }

    return strings.Join(all, "\n")
}

/* requireFreeExamplePort refuses to start on an occupied port: a leftover application from an interrupted run would answer every probe below and the section would pass without ever exercising the freshly built binary. */
func requireFreeExamplePort(major exampleMajor) {
    connection, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", major.port), time.Second)
    if nil != dialErr {
        return
    }

    _ = connection.Close()

    fail(
        "[%s] example application: port %d is already served — a leftover process from an interrupted run would answer for the binary under test; kill it first",
        major.label,
        major.port,
    )
}

func startExampleApplication(major exampleMajor, workspace string) *exampleApplication {
    logPath := filepath.Join(workspace, "e2e-application.log")

    logFile, createErr := os.Create(logPath)
    if nil != createErr {
        fail("[%s] example application: create the application log: %v", major.label, createErr)
    }
    defer func() {
        _ = logFile.Close()
    }()

    command := exec.Command(filepath.Join(workspace, "application"))
    command.Dir = workspace
    command.Env = exampleRunEnvironment()
    command.Stdout = logFile
    command.Stderr = logFile

    if startErr := command.Start(); nil != startErr {
        fail("[%s] example application: start the built binary: %v", major.label, startErr)
    }

    application := &exampleApplication{
        major:    major,
        command:  command,
        exitList: make(chan error, 1),
        logPath:  logPath,
    }

    go func() {
        application.exitList <- command.Wait()
    }()

    return application
}

/* waitForExampleReadiness polls the public route until it answers, and gives up the moment the process exits — an application that died at boot must produce a boot diagnostic, not a readiness timeout. */
func waitForExampleReadiness(major exampleMajor, application *exampleApplication) {
    client := &http.Client{Timeout: 2 * time.Second}
    deadline := time.Now().Add(60 * time.Second)

    for time.Now().Before(deadline) {
        select {
        case exitErr := <-application.exitList:
            fail(
                "[%s] example application: the process exited during boot (%v), so nothing below was exercised\n%s",
                major.label,
                exitErr,
                application.logTail(20),
            )
        default:
        }

        response, responseErr := client.Get(major.baseUrl() + exampleReadinessRoute)
        if nil == responseErr {
            _, _ = io.Copy(io.Discard, response.Body)
            _ = response.Body.Close()

            if http.StatusOK == response.StatusCode {
                return
            }
        }

        time.Sleep(200 * time.Millisecond)
    }

    fail(
        "[%s] example application: %s never answered 200 within the readiness budget, so nothing below was exercised\n%s",
        major.label,
        exampleReadinessRoute,
        application.logTail(20),
    )
}

/* exampleClient drives one running example over HTTP. Redirects are never followed (the entry point's 302 and the logout redirect are the assertions themselves) and the cookie jar is what makes the login flow real: the session id travels back on the following requests exactly as a browser would send it. */
type exampleClient struct {
    major  exampleMajor
    client *http.Client
    jar    *cookiejar.Jar
}

func newExampleClient(major exampleMajor) *exampleClient {
    jar, jarErr := cookiejar.New(nil)
    if nil != jarErr {
        fail("[%s] example application: open a cookie jar: %v", major.label, jarErr)
    }

    return &exampleClient{
        major: major,
        jar:   jar,
        client: &http.Client{
            Timeout: 10 * time.Second,
            Jar:     jar,
            CheckRedirect: func(request *http.Request, previous []*http.Request) error {
                return http.ErrUseLastResponse
            },
        },
    }
}

type exampleResponse struct {
    statusCode int
    location   string
    body       string
    cookieList []*http.Cookie
    headerList http.Header
}

/* call issues one request against the running example. The path is joined to the base url as a raw string so an encoded traversal attempt ("%2e%2e") reaches the application still encoded instead of being folded away by the client. */
func (instance *exampleClient) call(method string, path string, accept string, contentType string, body string) exampleResponse {
    return instance.callWithHeaderList(method, path, accept, contentType, nil, body)
}

/* callWithHeaderList is call with arbitrary request headers on top, which is what the cors, gzip, api-key and forwarded-address probes speak through: an explicit Accept-Encoding also switches off the Go transport's transparent gunzip, so the wire bytes arrive as the server sent them. */
func (instance *exampleClient) callWithHeaderList(method string, path string, accept string, contentType string, headerList map[string]string, body string) exampleResponse {
    var reader io.Reader
    if "" != body {
        reader = strings.NewReader(body)
    }

    request, requestErr := http.NewRequest(method, instance.major.baseUrl()+path, reader)
    if nil != requestErr {
        fail("[%s] example application: build a %s %s request: %v", instance.major.label, method, path, requestErr)
    }

    if "" != accept {
        request.Header.Set("Accept", accept)
    }
    if "" != contentType {
        request.Header.Set("Content-Type", contentType)
    }
    for headerName, headerValue := range headerList {
        request.Header.Set(headerName, headerValue)
    }

    response, responseErr := instance.client.Do(request)
    if nil != responseErr {
        fail("[%s] example application: %s %s: %v", instance.major.label, method, path, responseErr)
    }
    defer func() {
        _ = response.Body.Close()
    }()

    payload, readErr := io.ReadAll(response.Body)
    if nil != readErr {
        fail("[%s] example application: read the %s %s body: %v", instance.major.label, method, path, readErr)
    }

    return exampleResponse{
        statusCode: response.StatusCode,
        location:   response.Header.Get("Location"),
        body:       string(payload),
        cookieList: response.Cookies(),
        headerList: response.Header,
    }
}

func (instance *exampleClient) sessionCookie() *http.Cookie {
    base, parseErr := url.Parse(instance.major.baseUrl())
    if nil != parseErr {
        fail("[%s] example application: parse the base url: %v", instance.major.label, parseErr)
    }

    for _, cookie := range instance.jar.Cookies(base) {
        if exampleSessionCookie == cookie.Name {
            return cookie
        }
    }

    return nil
}

/* the routes below are the surface all three example applications share, read off their own route tables
({,v2/,v3/}.example/route/route.go and config/http.go) rather than assumed:

  - /routes/ is public in every major and lists the route table, which makes it the readiness probe;
  - /health is the monitoring probe and is public in every major. It used to be public in v3 alone while
    v1 and v2 left it to the ROLE_USER catch-all, so a monitoring system asking those two for the process's
    readiness was answered with a redirect to the login page. Probing it anonymously here is what keeps the
    three from drifting apart again;
  - /index.html is the same resource "/" serves, through MELODY_STATIC_INDEX_FILE, and carries the same
    public policy. The root rule is anchored at "^/$", so the explicit spelling needs its own rule and used
    to fall to the catch-all: two spellings of one page under two different policies;
  - /categories/api/read/ is the ROLE_USER route, /users/api/read/ the ROLE_ADMIN one, and the seeded "user"
    account holds ROLE_USER only — which is what separates the 401 of an anonymous request from the 403 of an
    authenticated one without the role. */
const (
    exampleRoutesRoute        = "/routes/"
    exampleRoutesRouteNoSlash = "/routes"
    exampleMissingRoute       = "/routes/no-such-route/"
    exampleLoginRoute         = "/login/"
    exampleLogoutRoute        = "/logout/"
    exampleHealthRoute        = "/health"
    exampleIndexFileRoute     = "/index.html"
    exampleUserRoute          = "/categories/api/read/"
    exampleAdminRoute         = "/users/api/read/"
    exampleStaticAsset        = "/assets/app.css"
    exampleFrontendBundle     = "/assets/app.js"
)

/* the login-throttle probe stops here rather than looping forever: the examples allow 30 writes a minute, so a ceiling comfortably past it turns "the throttle is missing" into a failure with a number in it instead of a run that never ends. */
const exampleLoginThrottleAttemptCeiling = 60

/* the icons every page links in its head. They exist in exactly ONE place in the tree — <root>/.assets — and are git-ignored under every example, so nothing here is a second copy that could go stale. What puts them in public/ is assets/sync-icons.mjs, run as the prebuild step of `npm run build` and by the container entrypoint for each major, which is the same single command that produces the bundle. Asserting them is what defends that wiring: they used to be produced only by a hand-written copy loop inside the entrypoint, so a build from a clean clone answered 404 for all four while the packaging matrix promised a binary requiring "nothing else" at runtime. */
var exampleSyncedIconList = []string{
    "/favicon.ico",
    "/assets/favicon.svg",
    "/assets/logo.png",
    "/assets/apple-touch-icon.png",
}

func runExampleHttpAssertions(major exampleMajor, application *exampleApplication, redisAddress string, mysqlDsn string, postgresDsn string) {
    client := newExampleClient(major)

    assertExamplePublicRoutes(major, client)
    assertExampleBrowserAssets(major, client)
    assertExampleAnonymousRejection(major, client)
    assertExampleLoginFlow(major, client)

    if true == major.loginThrottleProbe {
        assertExampleLoginIsThrottled(major, client, redisAddress)
    }

    assertExampleStaticTraversal(major, client)

    /* the showcase probes run BEFORE the integration demos on purpose: the throttled writes they spend are reset by the rate-limit subsection in there, which clears the counters before its exact count */
    if true == major.showcaseProbes {
        runExampleShowcaseAssertions(major, application, redisAddress)
    }

    /* the demo routes sit under the example's ROLE_USER catch-all, so they are driven here — between the login and the logout — with the session the login flow established */
    if true == major.integrationDemos {
        runExampleIntegrationAssertions(major, client, application, redisAddress, mysqlDsn, postgresDsn)
    }

    assertExampleLogout(major, client)
}

func assertExamplePublicRoutes(major exampleMajor, client *exampleClient) {
    listing := client.call("GET", exampleRoutesRoute, "", "", "")
    if http.StatusOK != listing.statusCode {
        fail("[%s] %s answered %d, wanted 200 from the public route listing", major.label, exampleRoutesRoute, listing.statusCode)
    }
    /* a 200 alone would also come from an error page, so the assertion is that the route table itself is in the body — the application booted its routes, it did not merely open a socket */
    if false == strings.Contains(listing.body, "example.login.submit") {
        fail("[%s] %s answered 200 without the route table in it: %s", major.label, exampleRoutesRoute, exampleTruncate(listing.body))
    }
    pass("[%s] the public route listing served the booted route table", major.label)

    /* the url generator emits patterns WITHOUT the trailing slash, so the form it hands to a client has to resolve as well as the declared one */
    withoutSlash := client.call("GET", exampleRoutesRouteNoSlash, "", "", "")
    if http.StatusOK != withoutSlash.statusCode {
        fail(
            "[%s] %s (the form the url generator emits) answered %d while %s answered 200",
            major.label,
            exampleRoutesRouteNoSlash,
            withoutSlash.statusCode,
            exampleRoutesRoute,
        )
    }
    pass("[%s] both %s and the generator's %s resolve to the same route", major.label, exampleRoutesRoute, exampleRoutesRouteNoSlash)

    /* the 404 is probed under a PUBLIC prefix on purpose: anywhere else the ROLE_USER catch-all answers first and the not-found handler is never reached */
    missing := client.call("GET", exampleMissingRoute, "", "", "")
    if http.StatusNotFound != missing.statusCode {
        fail("[%s] %s answered %d, wanted 404 from the not-found handler", major.label, exampleMissingRoute, missing.statusCode)
    }
    pass("[%s] an unrouted path under a public prefix answered 404", major.label)

    /* the monitoring probe is asked the way a monitoring system asks: anonymously, and without claiming to be a browser. Both spellings of the answer it used to get are failures here — the 302 an html-accepting client received through the entry point, and the 401 everything else received — and the body is read because a 200 alone is also what an error page carries. */
    health := client.call("GET", exampleHealthRoute, "", "", "")
    if http.StatusOK != health.statusCode {
        fail(
            "[%s] %s answered %d to an anonymous probe, wanted 200: a monitoring system cannot authenticate, so the readiness route has to sit outside the catch-all",
            major.label,
            exampleHealthRoute,
            health.statusCode,
        )
    }
    if false == strings.Contains(health.body, "\"status\"") {
        fail("[%s] %s answered 200 without the health payload in it: %s", major.label, exampleHealthRoute, exampleTruncate(health.body))
    }
    pass("[%s] the monitoring probe answered %s anonymously", major.label, exampleHealthRoute)

    /* the index file and the root are one resource under two spellings, so they must carry one policy. The root rule is anchored at "^/$", which is why the explicit spelling needs its own rule and used to be answered by the ROLE_USER catch-all while "/" was public. */
    indexFile := client.call("GET", exampleIndexFileRoute, "", "", "")
    if http.StatusOK != indexFile.statusCode {
        fail(
            "[%s] %s answered %d while / is public: the two spellings of one page must carry one policy",
            major.label,
            exampleIndexFileRoute,
            indexFile.statusCode,
        )
    }
    pass("[%s] %s carries the same public policy as the root it serves", major.label, exampleIndexFileRoute)
}

func assertExampleAnonymousRejection(major exampleMajor, client *exampleClient) {
    anonymous := client.call("GET", exampleUserRoute, "", "", "")
    if http.StatusUnauthorized != anonymous.statusCode {
        fail("[%s] %s answered %d to an anonymous request, wanted 401", major.label, exampleUserRoute, anonymous.statusCode)
    }
    pass("[%s] the protected route rejected the anonymous request with 401", major.label)

    /* the same route asked for html goes through the entry point instead, which redirects to the login page */
    browser := client.call("GET", exampleUserRoute, "text/html", "", "")
    if http.StatusFound != browser.statusCode {
        fail("[%s] %s answered %d to an html request, wanted 302 from the login entry point", major.label, exampleUserRoute, browser.statusCode)
    }
    if false == strings.HasPrefix(browser.location, exampleLoginRoute) {
        fail("[%s] the login entry point redirected to %q, wanted %s", major.label, browser.location, exampleLoginRoute)
    }
    pass("[%s] an html request for the protected route redirected to %s", major.label, exampleLoginRoute)
}

func assertExampleLoginFlow(major exampleMajor, client *exampleClient) {
    rejected := client.call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody(exampleUsername, exampleWrongPassword))
    if http.StatusUnauthorized != rejected.statusCode {
        fail("[%s] login with a wrong password answered %d, wanted 401: %s", major.label, rejected.statusCode, exampleTruncate(rejected.body))
    }
    if nil != client.sessionCookie() {
        fail("[%s] login with a wrong password handed out a session cookie", major.label)
    }
    pass("[%s] login with a wrong password was refused and issued no session", major.label)

    accepted := client.call("POST", exampleLoginRoute, "", "application/json", exampleCredentialBody(exampleUsername, examplePassword))
    if http.StatusOK != accepted.statusCode {
        fail("[%s] login answered %d, wanted 200: %s", major.label, accepted.statusCode, exampleTruncate(accepted.body))
    }
    pass("[%s] login with the seeded credentials was accepted", major.label)

    issued := exampleSetCookie(accepted.cookieList)
    if nil == issued {
        fail("[%s] login answered 200 without setting the %s cookie, so no session was published", major.label, exampleSessionCookie)
    }
    if "" == issued.Value {
        fail("[%s] login set an empty %s cookie", major.label, exampleSessionCookie)
    }
    if false == issued.HttpOnly {
        fail("[%s] the session cookie was set without HttpOnly, so script can read the session id", major.label)
    }
    if http.SameSiteDefaultMode == issued.SameSite {
        fail("[%s] the session cookie carries no SameSite attribute", major.label)
    }
    pass("[%s] the session cookie is HttpOnly and carries SameSite=%s", major.label, exampleSameSiteName(issued.SameSite))

    if nil == client.sessionCookie() {
        fail("[%s] the cookie jar did not keep the session cookie, so the flow below would not be a session flow", major.label)
    }

    granted := client.call("GET", exampleUserRoute, "", "", "")
    if http.StatusOK != granted.statusCode {
        fail("[%s] %s answered %d after login, wanted 200 — the session did not survive the round trip", major.label, exampleUserRoute, granted.statusCode)
    }
    if false == strings.Contains(granted.body, "cat-1") {
        fail("[%s] %s answered 200 after login without its payload: %s", major.label, exampleUserRoute, exampleTruncate(granted.body))
    }
    pass("[%s] the session loaded from the cookie admitted the protected route", major.label)

    /* the seeded account holds ROLE_USER only, so the admin route must answer 403 and not 401: a 401 here would mean the session was not loaded at all and the run would be proving nothing about authorization */
    forbidden := client.call("GET", exampleAdminRoute, "", "", "")
    if http.StatusForbidden != forbidden.statusCode {
        fail(
            "[%s] %s answered %d to the ROLE_USER session, wanted 403 (401 would mean the session was not loaded)",
            major.label,
            exampleAdminRoute,
            forbidden.statusCode,
        )
    }
    pass("[%s] the admin route answered 403 to the authenticated non-admin session", major.label)
}

/* assertExampleBrowserAssets asserts the two halves of what a browser needs before any of the example's pages does anything at all, and it asserts them on CONTENT rather than on the status code: a static surface answers 200 for a file that is present and empty just as readily as for one that is whole.

   Neither half is committed: both are produced by `npm run build` in the example's assets/ directory — the icons copied out of the single source by sync-icons.mjs, the bundle emitted from app.ts by esbuild — which the container entrypoint runs for each major at startup.

   What the bundle half defends is the state it came out of: every page loaded /assets/app.js and drove every interaction through window.melodyExample.*, while no assets/ directory existed in v1 or v2 at all, so the file could not be produced by anything and the whole browser interface was dead behind five pages that rendered perfectly. A 404 here means that source, or the step that runs it, is gone again. */
func assertExampleBrowserAssets(major exampleMajor, client *exampleClient) {
    for _, iconPath := range exampleSyncedIconList {
        icon := client.call("GET", iconPath, "", "", "")

        if http.StatusOK != icon.statusCode {
            fail("[%s] %s answered %d, wanted 200 — the icon is copied out of <root>/.assets by assets/sync-icons.mjs, which `npm run build` runs", major.label, iconPath, icon.statusCode)
        }
        if 0 == len(icon.body) {
            fail("[%s] %s answered 200 with an empty body — the file is present and truncated, which every status-code check reports as served", major.label, iconPath)
        }
    }
    pass("[%s] the %d shared icons are served whole", major.label, len(exampleSyncedIconList))

    bundle := client.call("GET", exampleFrontendBundle, "", "", "")
    if http.StatusOK != bundle.statusCode {
        fail("[%s] %s answered %d, wanted 200 — the browser interface is dead without it; the bundle is built from assets/app.ts by `npm run build`", major.label, exampleFrontendBundle, bundle.statusCode)
    }

    /* the bundle's whole contract with the pages is the object it installs on window: the pages call window.melodyExample.http/ui/routing on every interaction, and a bundle that loads without installing it leaves every one of them throwing on an undefined read */
    for _, expected := range []string{"melodyExample", "melodyRoutes"} {
        if false == strings.Contains(bundle.body, expected) {
            fail("[%s] %s answered 200 but carries no %q — it is not the example's bundle", major.label, exampleFrontendBundle, expected)
        }
    }
    pass("[%s] the frontend bundle is served and installs window.melodyExample (%d bytes)", major.label, len(bundle.body))

    /* the manifest the bundle generates URLs from is spliced into every page. The RouteGenerator reads it as an OBJECT under a "routes" key; a bare array — the shape v1 and v2 used to emit — decodes into a value whose .routes is undefined, so the generator is built empty and throws on the first route name it is asked for. */
    loginPage := client.call("GET", exampleLoginRoute, "", "", "")
    if http.StatusOK != loginPage.statusCode {
        fail("[%s] %s answered %d, wanted 200", major.label, exampleLoginRoute, loginPage.statusCode)
    }
    if false == strings.Contains(loginPage.body, `window.melodyRoutes = JSON.parse('{"routes":[`) {
        fail("[%s] %s does not carry the route manifest in the envelope shape the frontend RouteGenerator reads", major.label, exampleLoginRoute)
    }
    if false == strings.Contains(loginPage.body, `src="`+exampleFrontendBundle+`"`) {
        fail("[%s] %s does not load %s, so nothing installs window.melodyExample", major.label, exampleLoginRoute, exampleFrontendBundle)
    }
    pass("[%s] the login page carries the route manifest envelope and loads the bundle", major.label)
}

/* assertExampleLoginIsThrottled proves the credential door sits behind the shared per-address write budget.

   The signal has to distinguish the throttle from every other refusal: an unthrottled login answers 401 to a wrong password however many times it is asked, so the assertion is that the answer CHANGES — 401 up to the allowance and 429 past it. A run that only checked "some request was refused" would pass with the throttle removed, because the wrong password refuses on its own.

   The budget is reset on the way in AND on the way out. On the way in because the login flow above already spent two writes against it and the exact exhaustion point is what the allowance means; on the way out because this is the only probe in the harness whose whole purpose is to leave the budget empty, and every section after it signs somebody in — the showcase probes and the session-restart probe do it through the same throttled door, so an exhausted budget left behind fails them with a 429 that says nothing about what they assert.

   The password is deliberately wrong: a correct one would rotate the session under the client's jar on every accepted attempt and leave the flow below signed in as somebody else's request.

   The allowance is read from the answer rather than hardcoded here: the two examples configure their own, and a harness constant would silently stop measuring the exhaustion point if either changed it. */
func assertExampleLoginIsThrottled(major exampleMajor, client *exampleClient, redisAddress string) {
    if "" == redisAddress {
        skip("[%s] REDIS_ADDRESS is cleared, so the login throttle has no limiter behind it and was not asserted", major.label)

        return
    }

    label := fmt.Sprintf("[%s] login throttle", major.label)
    prefix := "melody-example-" + major.label + ":rate_limit:"

    resetExampleRateLimitCounters(label, redisAddress, prefix)
    defer resetExampleRateLimitCounters(label, redisAddress, prefix)

    body := exampleCredentialBody(exampleUsername, exampleWrongPassword)

    acceptedCount := 0
    throttledAt := 0

    for attempt := 1; attempt <= exampleLoginThrottleAttemptCeiling; attempt++ {
        response := client.call("POST", exampleLoginRoute, "", "application/json", body)

        if http.StatusTooManyRequests == response.statusCode {
            throttledAt = attempt

            break
        }

        if http.StatusUnauthorized != response.statusCode {
            fail(
                "[%s] the %d%s login attempt answered %d, wanted 401 or 429: %s",
                major.label, attempt, exampleOrdinalSuffix(attempt), response.statusCode, exampleTruncate(response.body),
            )
        }

        acceptedCount = acceptedCount + 1
    }

    if 0 == throttledAt {
        fail(
            "[%s] %d wrong-password logins in a row were all answered 401 — the login submit is not behind the write budget",
            major.label, exampleLoginThrottleAttemptCeiling,
        )
    }

    if 0 == acceptedCount {
        fail(
            "[%s] the very first login attempt answered 429, so the budget was already spent and nothing about the login door was measured",
            major.label,
        )
    }

    pass("[%s] the login submit is throttled: %d attempts answered 401 and the next answered 429", major.label, acceptedCount)
}

/* exampleOrdinalSuffix keeps the diagnostic readable when it names which attempt broke the pattern. */
func exampleOrdinalSuffix(number int) string {
    if 11 <= number%100 && 13 >= number%100 {
        return "th"
    }

    switch number % 10 {
    case 1:
        return "st"
    case 2:
        return "nd"
    case 3:
        return "rd"
    }

    return "th"
}

/* assertExampleStaticTraversal probes the static surface with percent-encoded dot segments, which a client will not fold away. The served asset is asserted FIRST: without it a disabled static surface would answer every traversal probe with a 404 and the section would report a containment it never tested. */
func assertExampleStaticTraversal(major exampleMajor, client *exampleClient) {
    asset := client.call("GET", exampleStaticAsset, "", "", "")
    if http.StatusOK != asset.statusCode {
        fail("[%s] %s answered %d, wanted 200 — the static surface the traversal probes target is not being served", major.label, exampleStaticAsset, asset.statusCode)
    }
    pass("[%s] the static surface serves %s (the traversal probes below have a live target)", major.label, exampleStaticAsset)

    probeList := []struct {
        path   string
        leaked string
        what   string
    }{
        {path: "/assets/%2e%2e/%2e%2e/go.mod", leaked: "module github.com/precision-soft/melody", what: "the example's own go.mod one level above the public directory"},
        {path: "/assets/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/etc/passwd", leaked: "root:x:", what: "/etc/passwd through the assets prefix"},
        {path: "/%2e%2e%2f%2e%2e%2fetc/passwd", leaked: "root:x:", what: "/etc/passwd from the root of the application"},
    }

    for _, probe := range probeList {
        response := client.call("GET", probe.path, "", "", "")

        if http.StatusOK == response.statusCode {
            fail("[%s] %s answered 200 — an encoded traversal reached %s", major.label, probe.path, probe.what)
        }
        if true == strings.Contains(response.body, probe.leaked) {
            fail("[%s] %s leaked %s in a %d response", major.label, probe.path, probe.what, response.statusCode)
        }
    }

    pass("[%s] %d encoded ../ traversal attempts stayed inside the public directory", major.label, len(probeList))
}

func assertExampleLogout(major exampleMajor, client *exampleClient) {
    loggedOut := client.call("GET", exampleLogoutRoute, "", "", "")
    if http.StatusFound != loggedOut.statusCode {
        fail("[%s] %s answered %d, wanted 302", major.label, exampleLogoutRoute, loggedOut.statusCode)
    }
    if "/" != loggedOut.location {
        fail("[%s] %s redirected to %q, wanted /", major.label, exampleLogoutRoute, loggedOut.location)
    }
    pass("[%s] logout redirected to /", major.label)

    /* the jar still holds the cookie, which is the point: the session it names must no longer authenticate anything, so the protected route has to answer 401 again */
    after := client.call("GET", exampleUserRoute, "", "", "")
    if http.StatusUnauthorized != after.statusCode {
        fail(
            "[%s] %s answered %d after logout, wanted 401 — the session id the client still holds still authenticates",
            major.label,
            exampleUserRoute,
            after.statusCode,
        )
    }
    pass("[%s] the session the client still names no longer admits the protected route", major.label)
}

func exampleCredentialBody(username string, password string) string {
    payload, marshalErr := json.Marshal(map[string]string{
        "username": username,
        "password": password,
    })
    if nil != marshalErr {
        fail("example application: encode the login body: %v", marshalErr)
    }

    return string(payload)
}

func exampleSetCookie(cookieList []*http.Cookie) *http.Cookie {
    for _, cookie := range cookieList {
        if exampleSessionCookie == cookie.Name {
            return cookie
        }
    }

    return nil
}

func exampleSameSiteName(sameSite http.SameSite) string {
    switch sameSite {
    case http.SameSiteLaxMode:
        return "Lax"
    case http.SameSiteStrictMode:
        return "Strict"
    case http.SameSiteNoneMode:
        return "None"
    default:
        return "Default"
    }
}

func exampleTruncate(body string) string {
    body = strings.ReplaceAll(body, "\n", " ")
    if 200 < len(body) {
        return body[:200] + "…"
    }

    return body
}

/* exampleAnsiPattern strips the cursor and colour sequences the command output frames its sections with, so the assertions match on the text a reader sees rather than on the escapes around it. */
var exampleAnsiPattern = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

/* runExampleCliAssertions exercises the command-line half of the same application. product:list declares no flags in the v2 example and only its own --limit in v3, so the standard set is legitimately unknown there; the flag layer is exercised through the framework's own debug:router instead, which declares the standard --format/--limit set in every major. The v1 example commands carry the standard flag set themselves, and their process-side envelope is asserted by the V1 sections of stack.sh. */
func runExampleCliAssertions(major exampleMajor, workspace string) {
    information := runExampleCommand(major, workspace, "app:info")

    expectedAddress := fmt.Sprintf("http_address: :%d", major.port)
    if false == strings.Contains(information, expectedAddress) {
        fail(
            "[%s] app:info reported no %q, so the workspace .env.local never reached the running configuration:\n%s",
            major.label,
            expectedAddress,
            exampleTail(information, 20),
        )
    }
    if false == exampleReportsPositiveCount(information, "routes:") {
        fail("[%s] app:info reported no routes, so the command booted an empty application:\n%s", major.label, exampleTail(information, 20))
    }
    pass("[%s] app:info booted the same configuration over the command line (http_address :%d)", major.label, major.port)

    products := runExampleCommand(major, workspace, "product:list")
    for _, expected := range []string{"NAME", "CREATED_AT", "prod-1"} {
        if false == strings.Contains(products, expected) {
            fail("[%s] product:list printed no %q:\n%s", major.label, expected, exampleTail(products, 20))
        }
    }
    pass("[%s] product:list resolved its services and printed the seeded catalogue", major.label)

    routerOutput := runExampleCommand(major, workspace, "debug:router", "--format=json", "--limit=2")

    var envelope struct {
        Data struct {
            Items []struct {
                Name    string `json:"name"`
                Pattern string `json:"pattern"`
            } `json:"items"`
            Total int `json:"total"`
            Limit int `json:"limit"`
        } `json:"data"`
    }

    document := exampleJsonDocument(routerOutput)
    if decodeErr := json.NewDecoder(strings.NewReader(document)).Decode(&envelope); nil != decodeErr {
        fail("[%s] debug:router --format=json emitted no decodable envelope (%v):\n%s", major.label, decodeErr, exampleTail(routerOutput, 20))
    }

    if 2 != envelope.Data.Limit {
        fail("[%s] debug:router echoed limit=%d, wanted the 2 that was passed", major.label, envelope.Data.Limit)
    }
    if 2 != len(envelope.Data.Items) {
        fail("[%s] debug:router --limit=2 emitted %d items, wanted 2", major.label, len(envelope.Data.Items))
    }
    /* a total that is not larger than the limit would make the truncation vacuous: the flag has to have cut something away for this to prove it was honoured */
    if 2 >= envelope.Data.Total {
        fail("[%s] debug:router reported total=%d, so --limit=2 truncated nothing", major.label, envelope.Data.Total)
    }
    pass("[%s] debug:router --format=json --limit=2 truncated %d routes to 2 (the flag layer)", major.label, envelope.Data.Total)
}

func runExampleCommand(major exampleMajor, workspace string, arguments ...string) string {
    command := exec.Command(filepath.Join(workspace, "application"), arguments...)
    command.Dir = workspace
    command.Env = exampleRunEnvironment()

    output, runErr := command.CombinedOutput()
    plain := exampleStripAnsi(string(output))

    if nil != runErr {
        fail("[%s] %s exited %v:\n%s", major.label, strings.Join(arguments, " "), runErr, exampleTail(plain, 20))
    }

    return plain
}

/* exampleJsonDocument isolates the command's json envelope from the boot log lines printed before it: the configuration is logged to stdout before the file logger exists, and those lines are json objects of their own. Under --format=json the envelope is one line like every log line above it, so what tells them apart is the meta key no log record carries; the last such line is the document, since the command writes it after its own boot. The brace fallback below reads a --format=json-pretty document, whose opening brace is the only line that is exactly "{". */
func exampleJsonDocument(output string) string {
    lines := strings.Split(output, "\n")

    for index := len(lines) - 1; 0 <= index; index = index - 1 {
        trimmed := strings.TrimSpace(lines[index])
        if false == strings.HasPrefix(trimmed, "{") {
            continue
        }

        probe := struct {
            Meta *struct {
                Command string `json:"command"`
            } `json:"meta"`
        }{}

        if decodeErr := json.Unmarshal([]byte(trimmed), &probe); nil != decodeErr {
            continue
        }

        if nil == probe.Meta {
            continue
        }

        return trimmed
    }

    for index, line := range lines {
        if "{" == strings.TrimSpace(line) {
            return strings.Join(lines[index:], "\n")
        }
    }

    return output
}

func exampleReportsPositiveCount(output string, label string) bool {
    for _, line := range strings.Split(output, "\n") {
        trimmed := strings.TrimSpace(line)
        if false == strings.HasPrefix(trimmed, label) {
            continue
        }

        count, convertErr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(trimmed, label)))
        if nil == convertErr && 0 < count {
            return true
        }
    }

    return false
}

func exampleTail(output string, lines int) string {
    all := strings.Split(strings.TrimRight(output, "\n"), "\n")
    if len(all) > lines {
        all = all[len(all)-lines:]
    }

    return strings.Join(all, "\n")
}

/* assertExampleGracefulShutdown proves the first signal is enough. The liveness probe is taken immediately before the signal because an exit-zero from an application that had already left on its own would satisfy the status assertion while proving nothing about the graceful path. */
func assertExampleGracefulShutdown(major exampleMajor, application *exampleApplication) {
    client := &http.Client{Timeout: 2 * time.Second}

    response, responseErr := client.Get(major.baseUrl() + exampleReadinessRoute)
    if nil != responseErr {
        fail("[%s] example application: the application had already stopped serving before the SIGINT (%v), so the graceful path was not exercised", major.label, responseErr)
    }
    _, _ = io.Copy(io.Discard, response.Body)
    _ = response.Body.Close()

    if signalErr := application.command.Process.Signal(os.Interrupt); nil != signalErr {
        fail("[%s] example application: send SIGINT: %v", major.label, signalErr)
    }

    select {
    case exitErr := <-application.exitList:
        if nil != exitErr {
            fail("[%s] one SIGINT while serving http exited %v, wanted a zero exit\n%s", major.label, exitErr, application.logTail(20))
        }

        pass("[%s] one SIGINT while serving http exited 0 with no SIGKILL needed", major.label)
    case <-time.After(30 * time.Second):
        application.kill()
        <-application.exitList

        fail("[%s] the application was still running 30s after one SIGINT and had to be SIGKILLed\n%s", major.label, application.logTail(20))
    }
}
