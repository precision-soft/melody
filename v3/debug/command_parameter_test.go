package debug

import (
    "encoding/json"
    "fmt"
    "strings"
    "testing"

    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    "github.com/precision-soft/melody/v3/container"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* @info the command exists to tell an operator whether a parameter carries a value, so redaction has to keep that answer while withholding the credential itself */
func TestRedactedParameterValue_HidesASecretButStillReportsWhetherItIsSet(t *testing.T) {
    cases := []struct {
        name     string
        value    any
        isSecret bool
        expected string
    }{
        {"secretWithValue", "P4ssPhrase", true, redactedValuePlaceholder},
        {"secretWithoutValue", "", true, redactedEmptyPlaceholder},
        {"ordinaryValue", "https://api.example.test", false, "https://api.example.test"},
        {"ordinaryEmptyValue", "", false, ""},
        {"secretNonStringValue", 12345, true, redactedValuePlaceholder},
        {"ordinaryNonStringValue", 12345, false, "12345"},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            redacted := redactedParameterValue(testCase.value, testCase.isSecret)

            if testCase.expected != redacted {
                t.Fatalf("expected %q, got %q", testCase.expected, redacted)
            }
        })
    }
}

/* @info the redaction must not leak the length either: on a short credential it narrows the search meaningfully */
func TestRedactedParameterValue_DoesNotLeakTheSecretOrItsLength(t *testing.T) {
    secretValue := "P4ssPhrase"

    redacted := redactedParameterValue(secretValue, true)

    if true == strings.Contains(redacted, secretValue) {
        t.Fatalf("the redacted value leaked the credential")
    }

    if len(secretValue) == len(redacted) {
        t.Fatalf("the redacted value leaked the credential length")
    }

    if redacted != redactedParameterValue("a", true) {
        t.Fatalf("the redacted value varies with the credential length")
    }
}

type parameterCommandTestEnvelope struct {
    Data struct {
        Items []struct {
            Name     string `json:"name"`
            IsSecret bool   `json:"isSecret"`
        } `json:"items"`
        Total  int `json:"total"`
        Limit  int `json:"limit"`
        Offset int `json:"offset"`
    } `json:"data"`
}

type parameterTestEnvironmentSource struct {
    value map[string]string
}

func (instance *parameterTestEnvironmentSource) Load() (map[string]string, error) {
    return instance.value, nil
}

var _ configcontract.EnvironmentSource = (*parameterTestEnvironmentSource)(nil)

func newParameterTestRuntime(t *testing.T) *testRuntime {
    t.Helper()

    environment, environmentErr := config.NewEnvironment(
        &parameterTestEnvironmentSource{
            value: map[string]string{
                config.EnvKey: config.EnvDevelopment,
            },
        },
    )
    if nil != environmentErr {
        t.Fatalf("failed to build the test environment: %v", environmentErr)
    }

    applicationConfiguration, configurationErr := config.NewConfiguration(environment, "/tmp/melody")
    if nil != configurationErr {
        t.Fatalf("failed to build the test configuration: %v", configurationErr)
    }

    /* the window is asserted against the command's own ascending output rather than against these names, so the assertions never depend on which parameters melody registers by default */
    for index := 0; index < 12; index++ {
        applicationConfiguration.RegisterRuntime(
            fmt.Sprintf("zz.window.%02d", index),
            fmt.Sprintf("value-%02d", index),
        )
    }

    applicationConfiguration.RegisterRuntimeSecret("zz.window.secret", "P4ssPhrase")

    serviceContainer := container.NewContainer()

    serviceContainer.MustRegister(
        config.ServiceConfig,
        func(resolver containercontract.Resolver) (configcontract.Configuration, error) {
            return applicationConfiguration, nil
        },
    )

    return newTestRuntime(serviceContainer)
}

func decodeParameterCommandEnvelope(t *testing.T, rendered string) parameterCommandTestEnvelope {
    t.Helper()

    envelope := parameterCommandTestEnvelope{}

    decodeErr := json.Unmarshal([]byte(rendered), &envelope)
    if nil != decodeErr {
        t.Fatalf("failed to decode the rendered envelope: %v, rendered %q", decodeErr, rendered)
    }

    return envelope
}

func parameterCommandNameList(t *testing.T, arguments []string) ([]string, int) {
    t.Helper()

    rendered, runErr := runDebugCommand(
        &ParameterCommand{},
        newParameterTestRuntime(t),
        arguments,
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    envelope := decodeParameterCommandEnvelope(t, rendered)

    name := make([]string, 0, len(envelope.Data.Items))
    for _, item := range envelope.Data.Items {
        name = append(name, item.Name)
    }

    return name, envelope.Data.Total
}

func assertParameterNameList(t *testing.T, expected []string, actual []string) {
    t.Helper()

    if len(expected) != len(actual) {
        t.Fatalf("expected %v, got %v", expected, actual)
    }

    for position, value := range expected {
        if value != actual[position] {
            t.Fatalf("expected %v, got %v", expected, actual)
        }
    }
}

func TestParameterCommand_ReportsEveryParameterWithoutAWindow(t *testing.T) {
    name, total := parameterCommandNameList(t, []string{"--format=json"})

    if total != len(name) {
        t.Fatalf("expected the unwindowed list to carry every item, got %d of %d", len(name), total)
    }
    if 8 > total {
        t.Fatalf("expected the fixture to register enough parameters to window, got %d", total)
    }
}

func TestParameterCommand_AppliesLimitAndOffsetToTheRenderedItems(t *testing.T) {
    ascending, total := parameterCommandNameList(t, []string{"--format=json"})

    windowed, windowedTotal := parameterCommandNameList(t, []string{"--format=json", "--limit=3", "--offset=2"})

    if total != windowedTotal {
        t.Fatalf("expected the reported total to stay at %d, got %d", total, windowedTotal)
    }

    assertParameterNameList(t, ascending[2:5], windowed)
}

func TestParameterCommand_ReturnsNoItemsForAnOffsetPastTheTotal(t *testing.T) {
    _, total := parameterCommandNameList(t, []string{"--format=json"})

    name, windowedTotal := parameterCommandNameList(
        t,
        []string{"--format=json", fmt.Sprintf("--offset=%d", total+5)},
    )

    if total != windowedTotal {
        t.Fatalf("expected the reported total to stay at %d, got %d", total, windowedTotal)
    }
    if 0 != len(name) {
        t.Fatalf("expected no items past the total, got %v", name)
    }
}

func TestParameterCommand_WalksEveryItemExactlyOnceWhenPaging(t *testing.T) {
    ascending, total := parameterCommandNameList(t, []string{"--format=json"})

    seen := map[string]int{}
    offset := 0

    for pageIndex := 0; pageIndex <= total; pageIndex++ {
        name, _ := parameterCommandNameList(
            t,
            []string{"--format=json", "--limit=4", fmt.Sprintf("--offset=%d", offset)},
        )

        if 0 == len(name) {
            break
        }

        for _, value := range name {
            seen[value] = seen[value] + 1
        }

        offset = offset + 4
    }

    if len(ascending) != len(seen) {
        t.Fatalf("expected paging to walk %d distinct parameters, got %d", len(ascending), len(seen))
    }

    for value, count := range seen {
        if 1 != count {
            t.Fatalf("parameter %q was returned %d times while paging", value, count)
        }
    }
}

func TestParameterCommand_ReversesTheItemsForADescendingOrder(t *testing.T) {
    ascending, _ := parameterCommandNameList(t, []string{"--format=json", "--order=asc"})
    descending, _ := parameterCommandNameList(t, []string{"--format=json", "--order=desc"})

    if len(ascending) != len(descending) {
        t.Fatalf("expected %d items, got %d", len(ascending), len(descending))
    }

    for position, value := range ascending {
        if value != descending[len(descending)-1-position] {
            t.Fatalf("expected the descending order to be the reverse of %v, got %v", ascending, descending)
        }
    }
}

func TestParameterCommand_AppliesTheDescendingOrderBeforeTheWindow(t *testing.T) {
    ascending, _ := parameterCommandNameList(t, []string{"--format=json"})

    windowed, _ := parameterCommandNameList(t, []string{"--format=json", "--order=desc", "--limit=3"})

    expected := []string{
        ascending[len(ascending)-1],
        ascending[len(ascending)-2],
        ascending[len(ascending)-3],
    }

    assertParameterNameList(t, expected, windowed)
}

func TestParameterCommand_WalksEveryItemExactlyOnceWhenPagingDescending(t *testing.T) {
    ascending, total := parameterCommandNameList(t, []string{"--format=json"})

    seen := map[string]int{}
    offset := 0

    for pageIndex := 0; pageIndex <= total; pageIndex++ {
        name, _ := parameterCommandNameList(
            t,
            []string{"--format=json", "--order=desc", "--limit=4", fmt.Sprintf("--offset=%d", offset)},
        )

        if 0 == len(name) {
            break
        }

        for _, value := range name {
            seen[value] = seen[value] + 1
        }

        offset = offset + 4
    }

    if len(ascending) != len(seen) {
        t.Fatalf("expected paging to walk %d distinct parameters, got %d", len(ascending), len(seen))
    }

    for value, count := range seen {
        if 1 != count {
            t.Fatalf("parameter %q was returned %d times while paging descending", value, count)
        }
    }
}

func TestParameterCommand_AppliesTheSameWindowInTheTableFormat(t *testing.T) {
    ascending, total := parameterCommandNameList(t, []string{"--format=json"})

    rendered, runErr := runDebugCommand(
        &ParameterCommand{},
        newParameterTestRuntime(t),
        []string{"--format=table", "--table-width=400", "--limit=3", "--offset=2"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    row := debugTableBlockRow(rendered, "PARAMETERS")

    if 3 != len(row) {
        t.Fatalf("expected 3 table rows for --limit=3, got %d, rendered %q", len(row), rendered)
    }
    if ascending[2] != row[0][0] {
        t.Fatalf("expected the table window to start at --offset=2, got %q", row[0][0])
    }

    if false == strings.Contains(rendered, fmt.Sprintf("PARAMETERS: %d total | 3 shown", total)) {
        t.Fatalf("expected the summary to report the shown count, got %q", rendered)
    }
}

func TestParameterCommand_KeepsRedactingASecretInsideTheWindow(t *testing.T) {
    rendered, runErr := runDebugCommand(
        &ParameterCommand{},
        newParameterTestRuntime(t),
        []string{"--format=json", "--order=desc", "--limit=5"},
    )
    if nil != runErr {
        t.Fatalf("expected no error, got %v", runErr)
    }

    if true == strings.Contains(rendered, "P4ssPhrase") {
        t.Fatalf("the windowed output leaked the credential: %q", rendered)
    }

    envelope := decodeParameterCommandEnvelope(t, rendered)

    hasSecret := false
    for _, item := range envelope.Data.Items {
        if true == item.IsSecret {
            hasSecret = true
        }
    }

    if false == hasSecret {
        t.Fatalf("expected the descending window to contain the secret parameter, got %q", rendered)
    }
}
