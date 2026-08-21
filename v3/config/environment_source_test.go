package config

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "testing"

    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/exception"
)

func TestEnvironmentContractIsUsed(t *testing.T) {
    var _ configcontract.EnvironmentSource = (*testEnvironmentSource)(nil)
}

/* godotenv understands a quoted value that spans lines, so the quote state of the comment stripper spans them too. */
func TestPreprocessDotEnvContent_KeepsMultilineQuotedValues(t *testing.T) {
    content := "KEY=\"first\n# not a comment\n\nlast\"\nOTHER=plain # trailing comment\n"

    processed, err := preprocessDotEnvContent(content)
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    if false == strings.Contains(processed, "# not a comment") {
        t.Fatalf("a '#' inside a quoted value is data, not a comment: %q", processed)
    }
    if false == strings.Contains(processed, "last\"") {
        t.Fatalf("the quoted value lost its tail: %q", processed)
    }

    /* the trailing comment stays in the produced line on purpose: godotenv performs its own countback on it, and cutting here as well would cut twice */
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "OTHER=plain # trailing comment\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if "plain" != values["OTHER"] {
        t.Fatalf("expected godotenv's countback to strip the real trailing comment, got %q", values["OTHER"])
    }
}

/* an editor that saves .env as UTF-8 with a byte order mark makes godotenv reject the first line, and U+FEFF is not whitespace, so no TrimSpace removes it. */
func TestPreprocessDotEnvContent_StripsTheByteOrderMark(t *testing.T) {
    processed, err := preprocessDotEnvContent("\ufeffMELODY_ENV=prod\nFOO=bar\n")
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    if true == strings.ContainsRune(processed, '\ufeff') {
        t.Fatalf("the byte order mark survived preprocessing: %q", processed)
    }
    if false == strings.HasPrefix(processed, "MELODY_ENV=prod") {
        t.Fatalf("expected the first assignment to be parseable, got %q", processed)
    }
}

/* godotenv skips a backslash-escaped quote inside a value, so it does not terminate the string and must not toggle the stripper's quote state either. */
func TestPreprocessDotEnvContent_KeepsBackslashEscapedQuotesInsideValues(t *testing.T) {
    content := `CONFIG="prefix \"section # note\" suffix"` + "\n"

    processed, err := preprocessDotEnvContent(content)
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    expected := `CONFIG="prefix \"section # note\" suffix"`
    if expected != processed {
        t.Fatalf("an escaped quote must not terminate the value, so the '#' inside it is data: expected %q, got %q", expected, processed)
    }
}

/* godotenv opens a quoted value only when the quote is the first character of the value; a stray quote in an unquoted value is literal. */
func TestPreprocessDotEnvContent_LiteralQuoteInUnquotedValueDoesNotSpanLines(t *testing.T) {
    content := "NOTE=say \"hello\nB=\"line1\n# data line\nline2\"\n"

    processed, err := preprocessDotEnvContent(content)
    if nil != err {
        t.Fatalf("preprocess: %v", err)
    }

    if false == strings.Contains(processed, "# data line") {
        t.Fatalf("a stray quote in NOTE must not flip cross-line state and drop B's interior line: %q", processed)
    }
    if false == strings.Contains(processed, "NOTE=say \"hello") {
        t.Fatalf("the unquoted NOTE value with a literal quote must be preserved: %q", processed)
    }
}

func TestLoadExistingDotEnvFile_PreservesQuotedWhitespace(t *testing.T) {
    directory := t.TempDir()

    writeErr := os.WriteFile(filepath.Join(directory, ".env"), []byte("PADDED=\"  spaced  \"\n"), 0o600)
    if nil != writeErr {
        t.Fatalf("write env file: %s", writeErr.Error())
    }

    source := NewEnvironmentSource(os.DirFS(directory), "")
    values := make(map[string]string)

    if loadErr := source.loadExistingDotEnvFile(values, ".env"); nil != loadErr {
        t.Fatalf("load env file: %s", loadErr.Error())
    }

    if "  spaced  " != values["PADDED"] {
        t.Fatalf("expected quoted whitespace preserved, got %q", values["PADDED"])
    }
}

func TestPreprocessDotEnvContent_InlineHashWithoutLeadingSpaceIsKept(t *testing.T) {
    processed, err := preprocessDotEnvContent("COLOR=#ffffff\nPASSWORD=ab#cd")
    if nil != err {
        t.Fatalf("unexpected error: %s", err.Error())
    }

    expected := "COLOR=#ffffff\nPASSWORD=ab#cd"
    if expected != processed {
        t.Fatalf("expected %q, got %q", expected, processed)
    }
}

/* the preprocessor does not cut the trailing comment itself: godotenv performs its own countback on the produced line, and two cuts in a row read "hello # world # x" as "hello" where godotenv reads "hello # world". Only the whole-line comment is dropped here. */
func TestPreprocessDotEnvContent_WhitespacePrecededHashIsComment(t *testing.T) {
    processed, err := preprocessDotEnvContent("KEY=value # trailing comment\n# full line comment\nOTHER=1")
    if nil != err {
        t.Fatalf("unexpected error: %s", err.Error())
    }

    expected := "KEY=value # trailing comment\nOTHER=1"
    if expected != processed {
        t.Fatalf("expected %q, got %q", expected, processed)
    }

    source := writeDotEnvFiles(t, map[string]string{
        ".env": "KEY=value # trailing comment\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if "value" != values["KEY"] {
        t.Fatalf("expected godotenv's own countback to cut the trailing comment, got %q", values["KEY"])
    }
}

func TestLoad_CommentCutMatchesGodotenv(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "GREETING=hello # world # x\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if "hello # world" != values["GREETING"] {
        t.Fatalf("expected the godotenv reading of the doubled hash, got %q", values["GREETING"])
    }
}

func TestLoad_PreservesNonUtf8BytesInQuotedValues(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "DB_PASSWORD='caf\xe9'\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("load error: %v", loadErr)
    }
    if "caf\xe9" != values["DB_PASSWORD"] {
        t.Fatalf("expected the Latin-1 byte preserved exactly, got %q", values["DB_PASSWORD"])
    }
}

func TestLoad_RefusesMalformedBracedReference(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "DSN=mysql://root:${DB-PASS}@db/app\n",
    })

    _, loadErr := source.Load()
    if nil == loadErr {
        t.Fatalf("expected the malformed braced reference to be refused")
    }
    if false == strings.Contains(loadErr.Error(), "malformed reference in env file value") {
        t.Fatalf("expected the malformed reference report, got: %v", loadErr)
    }
    if true == strings.Contains(loadErr.Error(), "DB-PASS") {
        t.Fatalf("expected the braced content to stay out of the error")
    }
}

func TestLoad_ParseFailureCarriesNoFileContent(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "BROKEN$KEY=x\nDB_PASSWORD=hunter2\n",
    })

    _, loadErr := source.Load()
    if nil == loadErr {
        t.Fatalf("expected the malformed variable name to fail the parse")
    }

    /* the leak traveled through the cause chain the logger renders, never through Error() alone — so the assertion renders exactly what the logger renders */
    renderedLogContext := fmt.Sprintf("%v", exception.LogContext(loadErr, nil))
    if true == strings.Contains(renderedLogContext, "hunter2") {
        t.Fatalf("expected the neighboring credential to stay out of the rendered log context: %s", renderedLogContext)
    }
    if true == strings.Contains(renderedLogContext, "\x00") {
        t.Fatalf("expected the marker bytes to stay out of the rendered log context")
    }
}

func TestLoad_EnvironmentNameWindowIsNamed(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env":      "MELODY_ENV=$PICKED_ENV\n",
        ".env.prod": "PICKED_ENV=prod\n",
    })

    _, loadErr := source.Load()
    if nil == loadErr {
        t.Fatalf("expected the out-of-window reference to be refused")
    }
    if false == strings.Contains(loadErr.Error(), ".env and .env.local") {
        t.Fatalf("expected the window to be named, got: %v", loadErr)
    }
}

func writeDotEnvFiles(t *testing.T, files map[string]string) *EnvironmentSource {
    t.Helper()

    directory := t.TempDir()

    for name, content := range files {
        writeErr := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600)
        if nil != writeErr {
            t.Fatalf("write %s: %s", name, writeErr.Error())
        }
    }

    return NewEnvironmentSource(os.DirFS(directory), "")
}

/* godotenv resolves ${KEY} against the keys of the one file being parsed, which is why a reference across melody's four-file layout has to be resolved here instead; CONFIG.md promises an undefined key fails the boot rather than degrade to empty. */
func TestEnvironmentSource_ResolvesReferenceAcrossFiles(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env":       "DB_USER=app\nDB_PASS=secret\n",
        ".env.local": "DSN=postgres://${DB_USER}:${DB_PASS}@db/app\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "postgres://app:secret@db/app" != values["DSN"] {
        t.Fatalf("expected the cross-file references to resolve, got %q", values["DSN"])
    }
}

func TestEnvironmentSource_ResolvesForwardReferenceAcrossFiles(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env":       "DSN=postgres://${DB_USER}@db/app\n",
        ".env.local": "DB_USER=app\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "postgres://app@db/app" != values["DSN"] {
        t.Fatalf("expected the forward reference to resolve, got %q", values["DSN"])
    }
}

func TestEnvironmentSource_FailsOnUndefinedReference(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "DSN=postgres://${DB_USER}@db/app\n",
    })

    _, loadErr := source.Load()
    if nil == loadErr {
        t.Fatalf("expected an undefined reference to fail the boot")
    }

    if false == strings.Contains(loadErr.Error(), "undefined key referenced in env file") {
        t.Fatalf("unexpected error: %s", loadErr.Error())
    }
}

func TestEnvironmentSource_FailsOnSelfReference(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "PATH_LIST=${PATH_LIST}:/opt/bin\n",
    })

    _, loadErr := source.Load()
    if nil == loadErr {
        t.Fatalf("expected a self reference to fail the boot")
    }

    if false == strings.Contains(loadErr.Error(), "references itself") {
        t.Fatalf("unexpected error: %s", loadErr.Error())
    }
}

func TestEnvironmentSource_FailsOnMutualReference(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env":       "LEFT=${RIGHT}\n",
        ".env.local": "RIGHT=${LEFT}\n",
    })

    _, loadErr := source.Load()
    if nil == loadErr {
        t.Fatalf("expected a mutual reference to fail the boot")
    }

    if false == strings.Contains(loadErr.Error(), "circular reference between env file keys") {
        t.Fatalf("unexpected error: %s", loadErr.Error())
    }
}

func TestEnvironmentSource_KeepsEscapedDollarAsData(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "PASSWORD=pa\\$sword\nQUOTED=\"pa\\$s2\"\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "pa$sword" != values["PASSWORD"] {
        t.Fatalf("expected the escaped dollar to survive as data, got %q", values["PASSWORD"])
    }

    if "pa$s2" != values["QUOTED"] {
        t.Fatalf("expected the escaped dollar in a quoted value to survive as data, got %q", values["QUOTED"])
    }
}

func TestEnvironmentSource_LeavesSingleQuotedValueLiteral(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "DB_USER=app\nTEMPLATE='${DB_USER}'\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "${DB_USER}" != values["TEMPLATE"] {
        t.Fatalf("expected a single-quoted value to stay literal, got %q", values["TEMPLATE"])
    }
}

func TestEnvironmentSource_KeepsDollarThatOpensNoReference(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "AMOUNT=100$\nPRICE=$ 20\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "100$" != values["AMOUNT"] {
        t.Fatalf("expected a trailing dollar to stay data, got %q", values["AMOUNT"])
    }

    if "$ 20" != values["PRICE"] {
        t.Fatalf("expected a dollar followed by a space to stay data, got %q", values["PRICE"])
    }
}

func TestEnvironmentSource_ResolvesBareAndChainedReferences(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env":       "HOST=db\nPORT=5432\nAUTHORITY=$HOST:$PORT\n",
        ".env.local": "DSN=postgres://${AUTHORITY}/app\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "db:5432" != values["AUTHORITY"] {
        t.Fatalf("expected the bare references to resolve, got %q", values["AUTHORITY"])
    }

    if "postgres://db:5432/app" != values["DSN"] {
        t.Fatalf("expected the chained reference to resolve, got %q", values["DSN"])
    }
}

func TestEnvironmentSource_ResolvesReferenceInsideTheEnvironmentName(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env":      "TARGET=prod\nMELODY_ENV=${TARGET}\n",
        ".env.prod": "APP_TAG=production\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "production" != values["APP_TAG"] {
        t.Fatalf("expected the environment file selected by the resolved name to be loaded, got %q", values["APP_TAG"])
    }

    if "prod" != values[EnvKey] {
        t.Fatalf("expected the environment key itself to be resolved, got %q", values[EnvKey])
    }
}

/* a key name is what godotenv says it is — upper case, digits, underscore — so a dollar followed by anything else is data: `pa$sword` and `$1.50` are values, not references. */
func TestEnvironmentSource_KeepsALowerCaseDollarSequenceAsData(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "DB_PASSWORD=pa$sword\nPRICE=$1.50\nMIXED=pa$sWORD\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("a value carrying a literal dollar failed the load: %s", loadErr.Error())
    }

    if "pa$sword" != values["DB_PASSWORD"] {
        t.Fatalf("expected the literal dollar to survive as data, got %q", values["DB_PASSWORD"])
    }

    if "$1.50" != values["PRICE"] {
        t.Fatalf("expected a dollar before a digit to survive as data, got %q", values["PRICE"])
    }

    if "pa$sWORD" != values["MIXED"] {
        t.Fatalf("expected a dollar before a lower-case letter to survive as data whatever follows it, got %q", values["MIXED"])
    }
}

func TestEnvironmentSource_StillResolvesAnUpperCaseReference(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env": "DB_USER=app\nBARE=$DB_USER\nBRACED=${DB_USER}-1\n",
    })

    values, loadErr := source.Load()
    if nil != loadErr {
        t.Fatalf("unexpected load error: %s", loadErr.Error())
    }

    if "app" != values["BARE"] {
        t.Fatalf("expected the bare reference to resolve, got %q", values["BARE"])
    }

    if "app-1" != values["BRACED"] {
        t.Fatalf("expected the braced reference to resolve, got %q", values["BRACED"])
    }
}

/* the double stands in for the source this file mirrors, so it lives here; the other test files of the
package reach it from the package. */
type testEnvironmentSource struct {
    values map[string]string
    err    error
}

func (instance *testEnvironmentSource) Load() (map[string]string, error) {
    if nil != instance.err {
        return nil, instance.err
    }

    copied := make(map[string]string, len(instance.values))
    for key, value := range instance.values {
        copied[key] = value
    }

    return copied, nil
}
