package twofactor

import (
    "context"
    "database/sql"
    "database/sql/driver"
    "errors"
    "io"
    nethttp "net/http"
    "net/http/httptest"
    "strings"
    "sync"
    "testing"
    "time"

    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v4/encrypt"
    store2fa "github.com/precision-soft/melody/v4/.example/twofactor"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttp "github.com/precision-soft/melody/v4/http"
    melodysecurity "github.com/precision-soft/melody/v4/security"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

const fixtureCipherKeyId = "two-factor-test"

func useFixtureCipher() melodyencrypt.Cipher {
    cipher := melodyencrypt.NewCipher(melodyencrypt.NewStaticKeyProvider(melodyencrypt.KeyRing{
        CurrentKeyId: fixtureCipherKeyId,
        Keys:         map[string][]byte{fixtureCipherKeyId: []byte("two-factor-handler-test-key-32by")},
    }))
    melodyencrypt.NewModule(melodyencrypt.ModuleConfig{Cipher: cipher, SealBindings: store2fa.SealBindings()}).RegisterServices(fixtureServiceRegistrar{Container: melodycontainer.NewContainer(melodycontainer.ContainerConfig{})})

    return cipher
}

type fixtureServiceRegistrar struct {
    melodycontainercontract.Container
}

func (instance fixtureServiceRegistrar) RegisterService(serviceName string, provider any, options melodycontainercontract.RegisterOptions) {
    instance.MustRegister(serviceName, provider, options)
}

type enrollmentConnector struct {
    mutex      sync.Mutex
    statements []string

    enrolledIdentifier string
    sealedSecret       string
    sealedCodes        string

    pendingIdentifier    string
    pendingSealedSecret  string
    pendingSealedCodes   string
    pendingReplaces      bool
}

func (instance *enrollmentConnector) Connect(ctx context.Context) (driver.Conn, error) {
    return &enrollmentConnection{connector: instance}, nil
}

func (instance *enrollmentConnector) Driver() driver.Driver {
    return nil
}

func (instance *enrollmentConnector) record(statement string) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.statements = append(instance.statements, statement)
}

func (instance *enrollmentConnector) recorded() []string {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return append([]string{}, instance.statements...)
}

type enrollmentConnection struct {
    connector *enrollmentConnector
}

func (instance *enrollmentConnection) Prepare(query string) (driver.Stmt, error) {
    return nil, errors.New("prepared statements are not supported by the enrollment driver")
}

func (instance *enrollmentConnection) Close() error {
    return nil
}

func (instance *enrollmentConnection) Begin() (driver.Tx, error) {
    return enrollmentTransaction{}, nil
}

type enrollmentTransaction struct{}

func (instance enrollmentTransaction) Commit() error {
    return nil
}

func (instance enrollmentTransaction) Rollback() error {
    return nil
}

func (instance *enrollmentConnection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
    instance.connector.record(query)

    return driver.RowsAffected(1), nil
}

func (instance *enrollmentConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
    /* the mysql dialect reads the server's version when the handle is built; it is the dialect's question, not a door's */
    if "SELECT version()" == query {
        return &enrollmentRows{columns: []string{"version()"}, rows: [][]driver.Value{{"8.0.36"}}}, nil
    }

    instance.connector.record(query)

    if true == strings.HasPrefix(query, "SELECT EXISTS") {
        found := false
        if true == strings.Contains(query, "melody_example_v4_two_factor_pending") {
            found = "" != instance.connector.pendingIdentifier && true == strings.Contains(query, "'"+instance.connector.pendingIdentifier+"'")
        } else {
            found = "" != instance.connector.enrolledIdentifier && true == strings.Contains(query, "'"+instance.connector.enrolledIdentifier+"'")
        }

        return &enrollmentRows{columns: []string{"exists"}, rows: [][]driver.Value{{found}}}, nil
    }

    if true == strings.HasPrefix(query, "SELECT `user_identifier` FROM `melody_example_v4_two_factor` ") {
        lockedRow := &enrollmentRows{columns: []string{"user_identifier"}}
        if "" != instance.connector.enrolledIdentifier && true == strings.Contains(query, "'"+instance.connector.enrolledIdentifier+"'") {
            lockedRow.rows = [][]driver.Value{{instance.connector.enrolledIdentifier}}
        }

        return lockedRow, nil
    }

    if true == strings.Contains(query, "melody_example_v4_two_factor_pending") {
        pendingRow := &enrollmentRows{columns: []string{"user_identifier", "secret", "recovery_codes", "replaces_factor", "created_at"}}
        if "" != instance.connector.pendingIdentifier && true == strings.Contains(query, "'"+instance.connector.pendingIdentifier+"'") {
            pendingRow.rows = [][]driver.Value{{
                instance.connector.pendingIdentifier,
                []byte(instance.connector.pendingSealedSecret),
                []byte(instance.connector.pendingSealedCodes),
                instance.connector.pendingReplaces,
                time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
            }}
        }

        return pendingRow, nil
    }

    enrolledRow := &enrollmentRows{columns: []string{"user_identifier", "secret", "recovery_codes", "created_at"}}
    if "" != instance.connector.enrolledIdentifier && true == strings.Contains(query, "'"+instance.connector.enrolledIdentifier+"'") {
        enrolledRow.rows = [][]driver.Value{{
            instance.connector.enrolledIdentifier,
            []byte(instance.connector.sealedSecret),
            []byte(instance.connector.sealedCodes),
            time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
        }}
    }

    return enrolledRow, nil
}

type enrollmentRows struct {
    columns []string
    rows    [][]driver.Value
    cursor  int
}

func (instance *enrollmentRows) Columns() []string {
    return instance.columns
}

func (instance *enrollmentRows) Close() error {
    return nil
}

func (instance *enrollmentRows) Next(destination []driver.Value) error {
    if instance.cursor >= len(instance.rows) {
        return io.EOF
    }

    copy(destination, instance.rows[instance.cursor])
    instance.cursor = instance.cursor + 1

    return nil
}

func enrolledStore(t *testing.T, enrolledIdentifier string, secret string) (*store2fa.Store, *enrollmentConnector) {
    t.Helper()

    useFixtureCipher()

    connector := &enrollmentConnector{enrolledIdentifier: enrolledIdentifier}
    if "" != enrolledIdentifier {
        sealedSecret, secretErr := melodyencrypt.EncryptedStringFor[store2fa.SecretColumn](secret).Value()
        if nil != secretErr {
            t.Fatalf("sealing the fixture secret failed: %v", secretErr)
        }

        sealedCodes, codesErr := melodyencrypt.EncryptedStringFor[store2fa.RecoveryCodesColumn](`["recovery-one"]`).Value()
        if nil != codesErr {
            t.Fatalf("sealing the fixture recovery codes failed: %v", codesErr)
        }

        connector.sealedSecret = string(sealedSecret.([]byte))
        connector.sealedCodes = string(sealedCodes.([]byte))
    }

    database := bun.NewDB(sql.OpenDB(connector), mysqldialect.New())
    t.Cleanup(func() {
        _ = database.Close()
    })

    return store2fa.NewStore(database), connector
}

func withPending(t *testing.T, connector *enrollmentConnector, pendingIdentifier string, secret string) {
    t.Helper()

    sealedSecret, secretErr := melodyencrypt.EncryptedStringFor[store2fa.PendingSecretColumn](secret).Value()
    if nil != secretErr {
        t.Fatalf("sealing the pending secret failed: %v", secretErr)
    }

    sealedCodes, codesErr := melodyencrypt.EncryptedStringFor[store2fa.PendingRecoveryCodesColumn](`["pending-one"]`).Value()
    if nil != codesErr {
        t.Fatalf("sealing the pending recovery codes failed: %v", codesErr)
    }

    connector.pendingIdentifier = pendingIdentifier
    connector.pendingSealedSecret = string(sealedSecret.([]byte))
    connector.pendingSealedCodes = string(sealedCodes.([]byte))
}

func authenticatedTwoFactorRequest(t *testing.T, target string, userIdentifier string, headers map[string]string) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    request, runtimeInstance := twoFactorRequest(t, target)
    for name, value := range headers {
        request.HttpRequest().Header.Set(name, value)
    }

    firewall := melodysecurity.NewCompiledFirewall(melodysecurity.CompiledFirewallConfig{
        Name:               "main",
        Matcher:            melodysecurity.NewPathPrefixMatcher("/"),
        MatcherDescription: "prefix /",
    })

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(userIdentifier, []string{"ROLE_USER"}, nil)),
    )

    return request, runtimeInstance
}

func twoFactorRequest(t *testing.T, target string) (*melodyhttp.Request, melodycontainercontract.Runtime) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    runtimeInstance := melodyruntime.NewRequest(context.Background(), containerInstance.NewScope())

    request := melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodPost, target, nil),
        runtimeInstance,
        melodyhttp.RequestConfig{
            RequestContext: melodyhttp.NewRequestContext("two-factor-test", time.Now()),
        },
    )

    return request, runtimeInstance
}

func fixedStore(store *store2fa.Store) store2fa.StoreSource {
    return func(runtimeInstance melodycontainercontract.Runtime) (*store2fa.Store, error) {
        return store, nil
    }
}

func insertStatements(statements []string) []string {
    insertList := make([]string, 0)
    for _, statement := range statements {
        if true == strings.HasPrefix(statement, "INSERT INTO ") {
            insertList = append(insertList, statement)
        }
    }

    return insertList
}
