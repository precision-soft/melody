package config

import (
    "os"
    "path/filepath"
    "strings"
    "testing"

    configcontract "github.com/precision-soft/melody/v2/config/contract"
)

func TestEnvironmentContractIsUsed(t *testing.T) {
    var _ configcontract.EnvironmentSource = (*testEnvironmentSource)(nil)
}

/* @info godotenv understands a quoted value that spans lines. The comment stripper tracked quote state per physical line, so from the second line on it believed it was outside quotes: a '#' inside the value opened a comment, and a line that became blank was dropped, silently truncating the value. */
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
    if true == strings.Contains(processed, "trailing comment") {
        t.Fatalf("a real trailing comment must still be stripped: %q", processed)
    }
}

/* @info An editor that saves .env as UTF-8 with a byte order mark makes godotenv reject the first line ("unexpected character in variable name"), so boot fails on a file that looks perfectly correct. U+FEFF is not whitespace, so no TrimSpace removes it. */
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

/* @info godotenv skips a backslash-escaped quote inside a value, so it does not terminate the string. The stripper toggled its quote state on every quote, so an escaped quote flipped it to "outside quotes"; a following space-preceded '#' was then read as a comment and the valid value was truncated. */
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

/* @info godotenv opens a quoted value only when the quote is the first character of the value; a stray quote in an unquoted value is literal. The stripper opened quote state on any quote, so an unbalanced quote in one value flipped the state for the rest of the file: a later genuinely-quoted multiline value then had a '#'-prefixed interior line silently dropped. */
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

func TestPreprocessDotEnvContent_WhitespacePrecededHashIsComment(t *testing.T) {
    processed, err := preprocessDotEnvContent("KEY=value # trailing comment\n# full line comment\nOTHER=1")
    if nil != err {
        t.Fatalf("unexpected error: %s", err.Error())
    }

    expected := "KEY=value\nOTHER=1"
    if expected != processed {
        t.Fatalf("expected %q, got %q", expected, processed)
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

/* @info godotenv resolves ${KEY} against the keys of the one file being parsed, so a reference across melody's four-file layout — .env holds the credential, .env.local assembles the connection string that reads it — became the empty string with nothing logged, and the application booted against "postgres://:@db/app". CONFIG.md promises the opposite for %env(): an undefined key fails the boot rather than degrade to empty. */
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

/* @info A forward reference — the referenced key defined in a file loaded later — is the same failure in the other direction. */
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

/* @info A reference to a key nobody defines must fail the boot, the same rule %env(KEY)% follows, instead of silently blanking the value. */
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

/* @info A key that reads itself has to be named as such rather than recurse forever or quietly blank. */
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

/* @info Two keys reading each other is the same problem one step further out. */
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

/* @info A backslash-escaped dollar is data: a password holding one must survive as written and must never be looked up as a reference. */
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

/* @info A single-quoted value is literal to godotenv, and it stays literal here: no reference is looked up inside one. */
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

/* @info A dollar that opens nothing name-shaped is data, so a lone one and one in front of a character no key may start with are both left alone rather than reported. */
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

/* @info The bare form is resolved as well, and a reference chain resolves through as data. */
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

/* @info The environment name picks the next two files to load, so a reference inside it is resolved against the two files already read. */
func TestEnvironmentSource_ResolvesReferenceInsideTheEnvironmentName(t *testing.T) {
    source := writeDotEnvFiles(t, map[string]string{
        ".env":       "TARGET=prod\nMELODY_ENV=${TARGET}\n",
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

/* @info A dollar followed by anything a key name may not start with is data, and a key name is what godotenv says it is: upper case, digits, underscore. `pa$sword` is a password that had always been read literally, and admitting lower case turned it into a reference to a key named `sword` that no file defines — which fails the boot of an application whose configuration never changed. The dot did the same to a value such as `$1.50` once it carried a digit. */
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

/* @info the control: an upper-case name after the dollar IS a reference, in both the bare and the braced form, so narrowing the character set did not disable the feature it belongs to. */
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
