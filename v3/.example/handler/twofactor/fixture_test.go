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

    store2fa "github.com/precision-soft/melody/v3/.example/twofactor"
    melodyencrypt "github.com/precision-soft/melody/integrations/bunorm/v3/encrypt"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
    "github.com/uptrace/bun"
    "github.com/uptrace/bun/dialect/mysqldialect"
)

const fixtureCipherKeyId = "two-factor-test"

/* useFixtureCipher installs the process-wide cipher the encrypted columns seal and open through, the way the composition root installs its own at boot. */
func useFixtureCipher() melodyencrypt.Cipher {
    cipher := melodyencrypt.NewCipher(melodyencrypt.NewStaticKeyProvider(
        fixtureCipherKeyId,
        map[string][]byte{fixtureCipherKeyId: []byte("two-factor-handler-test-key-32by")},
    ))
    melodyencrypt.UseCipher(cipher)

    return cipher
}

/* enrollmentConnector is a database holding one enrolled account: a select answers that account's row only when the statement names that account, and every statement is kept in order, so what a door read and what it wrote is the recorded sequence. bun renders its arguments into the statement text, which is what the assertions read. */
type enrollmentConnector struct {
    mutex      sync.Mutex
    statements []string

    enrolledIdentifier string
    sealedSecret       string
    sealedCodes        string
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

/* a transaction is accepted and changes nothing: the recovery door redeems inside one, and what it read and wrote is still the recorded sequence */
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

/* enrolledStore answers the real store over a database in which only the given account is enrolled, under the given secret. */
func enrolledStore(t *testing.T, enrolledIdentifier string, secret string) (*store2fa.Store, *enrollmentConnector) {
    t.Helper()

    cipher := useFixtureCipher()

    connector := &enrollmentConnector{enrolledIdentifier: enrolledIdentifier}
    if "" != enrolledIdentifier {
        sealedSecret, secretErr := cipher.Encrypt(secret)
        if nil != secretErr {
            t.Fatalf("sealing the fixture secret failed: %v", secretErr)
        }

        sealedCodes, codesErr := cipher.Encrypt(`["recovery-one"]`)
        if nil != codesErr {
            t.Fatalf("sealing the fixture recovery codes failed: %v", codesErr)
        }

        connector.sealedSecret = sealedSecret
        connector.sealedCodes = sealedCodes
    }

    database := bun.NewDB(sql.OpenDB(connector), mysqldialect.New())
    t.Cleanup(func() {
        _ = database.Close()
    })

    return store2fa.NewStore(database), connector
}

/* authenticatedTwoFactorRequest builds a request whose runtime carries a token for the given account, the way the firewall leaves it for a caller on the authenticated route. */
func authenticatedTwoFactorRequest(t *testing.T, target string, userIdentifier string, headers map[string]string) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    request, runtimeInstance := twoFactorRequest(t, target)
    for name, value := range headers {
        request.HttpRequest().Header.Set(name, value)
    }

    firewall := melodysecurity.NewCompiledFirewall(
        "main",
        melodysecurity.NewPathPrefixMatcher("/"),
        "prefix /",
        nil, nil, nil, nil, nil, nil, nil,
        "", "",
        nil, nil,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
        melodysecurity.SourceNone,
    )

    melodysecurity.SecurityContextSetOnRuntime(
        runtimeInstance,
        melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(userIdentifier, []string{"ROLE_USER"})),
    )

    return request, runtimeInstance
}

/* twoFactorRequest builds a request whose runtime carries NO security context, which is what an
   unauthenticated caller reaches these doors with. */
func twoFactorRequest(t *testing.T, target string) (*melodyhttp.Request, melodyruntimecontract.Runtime) {
    t.Helper()

    containerInstance := melodycontainer.NewContainer()
    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    request := melodyhttp.NewRequest(
        httptest.NewRequest(nethttp.MethodPost, target, nil),
        nil,
        runtimeInstance,
        melodyhttp.NewRequestContext("two-factor-test", time.Now()),
    )

    return request, runtimeInstance
}

/* fixedStore hands a door one store whatever the runtime, the source its tests need where production
   resolves the store through the container */
func fixedStore(store *store2fa.Store) store2fa.StoreSource {
    return func(runtimeInstance melodyruntimecontract.Runtime) (*store2fa.Store, error) {
        return store, nil
    }
}
