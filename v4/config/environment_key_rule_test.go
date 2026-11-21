package config

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
)

func TestLimitKeyRule_ReadsTheTextAsTheFrameworkReadsALimitKey(t *testing.T) {
    unlimitedAdmitted := LimitKeyRule(bound.Rule[time.Duration]{Default: time.Second, Minimum: time.Millisecond, Maximum: time.Minute, UnlimitedAdmitted: true})
    unlimitedRefused := LimitKeyRule(bound.Rule[time.Duration]{Default: time.Second, Minimum: time.Millisecond, Maximum: time.Minute})

    for _, testCase := range []struct {
        name     string
        rule     configcontract.EnvironmentKeyRule
        text     string
        expected string
    }{
        {"empty", unlimitedRefused, "", ""},
        {"padded empty", unlimitedRefused, "  ", ""},
        {"a value", unlimitedRefused, "2s", ""},
        {"unlimited admitted", unlimitedAdmitted, "unlimited", ""},
        {"unlimited refused", unlimitedRefused, "unlimited", "the limit does not admit unlimited"},
        {"below the minimum", unlimitedRefused, "0s", "the limit is below its minimum"},
        {"above the maximum", unlimitedRefused, "2m", "the limit is above its maximum"},
        {"not a duration", unlimitedRefused, "soon", "the limit is not a value of its unit"},
    } {
        checkErr := testCase.rule.Check(testCase.text, "x.F")
        if "" == testCase.expected {
            if nil != checkErr {
                t.Fatalf("%s: unexpected refusal %v", testCase.name, checkErr)
            }

            continue
        }

        if nil == checkErr || testCase.expected != checkErr.Error() || "x.F" != contextOfError(t, checkErr)["field"] {
            t.Fatalf("%s: expected %q naming the field, got %v", testCase.name, testCase.expected, checkErr)
        }
    }
}

func TestSwitchKeyRule_ReadsABooleanOrNothing(t *testing.T) {
    rule := SwitchKeyRule()

    for _, text := range []string{"", " ", "true", "false", "1", "off"} {
        if checkErr := rule.Check(text, "x.F"); nil != checkErr {
            t.Fatalf("%q: unexpected refusal %v", text, checkErr)
        }
    }

    if checkErr := rule.Check("treu", "x.F"); nil == checkErr || "x.F" != contextOfError(t, checkErr)["field"] {
        t.Fatalf("expected treu refused naming the field, got %v", checkErr)
    }
}

func TestChoiceKeyRule_AdmitsOneChoiceExactlyOrNothing(t *testing.T) {
    admitted := []string{"OnFailure", "Never"}
    rule := ChoiceKeyRule(admitted)
    admitted[0] = "Always"

    for _, text := range []string{"", "OnFailure", "Never"} {
        if checkErr := rule.Check(text, "x.F"); nil != checkErr {
            t.Fatalf("%q: unexpected refusal %v", text, checkErr)
        }
    }

    for _, text := range []string{"onfailure", "Always", " Never", "  "} {
        if checkErr := rule.Check(text, "x.F"); nil == checkErr || "x.F" != contextOfError(t, checkErr)["field"] {
            t.Fatalf("%q: expected a refusal naming the field, got %v", text, checkErr)
        }
    }

    if refusal := refusalOfRead(func() { ChoiceKeyRule(nil) }); nil == refusal {
        t.Fatalf("expected an empty choice list refused")
    }
}

func TestKeyRule_DescribeSeparatesTwoRules(t *testing.T) {
    descriptions := map[string]bool{}
    for _, rule := range []configcontract.EnvironmentKeyRule{
        LimitKeyRule(bound.Rule[int]{Default: 3, Minimum: 1}),
        LimitKeyRule(bound.Rule[int]{Default: 3, Minimum: 2}),
        LimitKeyRule(bound.Rule[int]{Default: 3, Minimum: 1, Maximum: 9}),
        LimitKeyRule(bound.Rule[int]{Default: 3, Minimum: 1, UnlimitedAdmitted: true}),
        SwitchKeyRule(),
        ChoiceKeyRule([]string{"a", "b"}),
        ChoiceKeyRule([]string{"a"}),
    } {
        if true == descriptions[rule.Describe()] {
            t.Fatalf("expected %q to describe one rule", rule.Describe())
        }

        descriptions[rule.Describe()] = true
    }
}
