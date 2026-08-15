package cron

import (
    "errors"
    "strings"
    "testing"
)

func TestValidateUserFieldRejectsForbiddenCharacter(t *testing.T) {
    if false == errors.Is(validateUserField("user", "svc%role"), ErrForbiddenCharacter) {
        t.Fatalf("expected ErrForbiddenCharacter for a user token containing a crontab line-continuation %%")
    }

    if nil != validateUserField("user", "deploy") {
        t.Fatalf("expected a normal user token to validate")
    }
}

func TestValidateNoForbiddenCharsRejectsForbiddenChar(t *testing.T) {
    err := ValidateNoForbiddenCharacters([]string{"clean", "with%percent"}, CrontabForbiddenCharacters, "test context")
    if nil == err {
        t.Fatalf("expected error for token containing %%")
    }

    if false == strings.Contains(err.Error(), "test context") {
        t.Fatalf("expected error to mention the context, got: %v", err)
    }

    if false == strings.Contains(err.Error(), "with%percent") {
        t.Fatalf("expected error to quote the offending token, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsAllowsCleanTokens(t *testing.T) {
    err := ValidateNoForbiddenCharacters([]string{"safe", "tokens", "only"}, CrontabForbiddenCharacters, "test context")
    if nil != err {
        t.Fatalf("expected nil error for clean tokens, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsWithCustomList(t *testing.T) {
    custom := []ForbiddenCharacter{
        {Char: '\t', Reason: "tabs break YAML"},
    }

    err := ValidateNoForbiddenCharacters([]string{"has\ttab"}, custom, "yaml entry")
    if nil == err {
        t.Fatalf("expected error for tab character")
    }

    if false == strings.Contains(err.Error(), "yaml entry") {
        t.Fatalf("expected error to mention the context, got: %v", err)
    }
}

func TestValidateNoForbiddenCharsEmptyTokensReturnsNil(t *testing.T) {
    err := ValidateNoForbiddenCharacters(nil, CrontabForbiddenCharacters, "test context")
    if nil != err {
        t.Fatalf("expected nil error for empty tokens, got: %v", err)
    }
}

/* crond treats one out-of-range field as a parse error and refuses the whole crontab file with it, so generation must fail on the same bounds the in-process matcher enforces */
func TestValidateScheduleFieldsRejectsOutOfRangeValues(t *testing.T) {
    cases := []struct {
        field    string
        schedule *Schedule
    }{
        {"Minute", &Schedule{Minute: "60"}},
        {"Hour", &Schedule{Hour: "24"}},
        {"DayOfMonth", &Schedule{DayOfMonth: "32"}},
        {"Month", &Schedule{Month: "13"}},
        {"DayOfWeek", &Schedule{DayOfWeek: "8"}},
        {"Minute range", &Schedule{Minute: "10-60"}},
        {"Minute integer", &Schedule{Minute: "1x"}},
        {"Month name", &Schedule{Month: "foo"}},
        {"Minute name", &Schedule{Minute: "mon"}},
        {"Month glued name", &Schedule{Month: "1jan"}},
        {"DayOfWeek step name", &Schedule{DayOfWeek: "*/mon"}},
    }

    for _, testCase := range cases {
        err := validateScheduleFields(Entry{Name: "job", Schedule: testCase.schedule}, CrontabForbiddenCharacters)
        if nil == err {
            t.Fatalf("expected an out-of-range %s to fail generation, got nil", testCase.field)
        }

        if false == errors.Is(err, ErrInvalidSchedule) {
            t.Fatalf("expected errors.Is(err, ErrInvalidSchedule) for %s, got: %v", testCase.field, err)
        }
    }
}

/* day of week 7 is the Sunday alias vixie crond accepts */
func TestValidateScheduleFieldsAcceptsValidShapes(t *testing.T) {
    cases := []*Schedule{
        {Minute: "5-59/15"},
        {Minute: "*/15", Hour: "9-17", DayOfMonth: "1,15", Month: "JAN,dec", DayOfWeek: "mon-fri"},
        {DayOfWeek: "sun-sat/2"},
        {Minute: "0", Hour: "3", DayOfMonth: "1", Month: "12", DayOfWeek: "7"},
        {},
    }

    for index, schedule := range cases {
        if err := validateScheduleFields(Entry{Name: "job", Schedule: schedule}, CrontabForbiddenCharacters); nil != err {
            t.Fatalf("expected schedule %d to pass, got: %v", index, err)
        }
    }
}

/* the user column sits on the generated line beside the schedule fields, and crond splits that line on any unicode space — so this guard reads the same rule the schedule parser reads rather than the four ascii spellings. */
func TestValidateUserField_RefusesEveryUnicodeSpace(t *testing.T) {
    spellings := map[string]string{
        "vertical tab": "deploy\vrole",
        "form feed":    "deploy\frole",
        "no-break":     "deploy role",
        "ideographic":  "deploy　role",
        "plain space":  "deploy role",
        "tab":          "deploy\trole",
        "newline":      "deploy\nrole",
        "carriage":     "deploy\rrole",
    }

    for spelling, value := range spellings {
        err := validateUserField("user", value)
        if nil == err {
            t.Fatalf("%s: a user carrying whitespace must be refused", spelling)
        }

        if false == errors.Is(err, ErrFieldContainsWhitespace) && false == errors.Is(err, ErrForbiddenCharacter) {
            t.Fatalf("%s: expected a whitespace refusal, got %v", spelling, err)
        }
    }
}

/* the divergent verdicts mirror the live measurement on busybox 1.37: DayOfWeek "0-6" beside DayOfMonth "16" ran only on the 16th under busybox while the crontab-dialect matcher answered due every day, and the check is semantic — both models evaluated over every day combination — so the agreeing pairs prove the refusal does not overreach. */
func TestBusyboxDayFieldsDiverge_FlagsThePairsBusyboxRunsDifferently(t *testing.T) {
    divergent := map[string][2]string{
        "full-coverage day of week beside a restricted day of month": {"16", "0-6"},
        "full-coverage via the sunday alias":                         {"16", "0-7"},
        "stepped wildcard day of month beside a restricted weekday":  {"*/2", "1"},
        "full-coverage day of month beside a restricted weekday":     {"1-31", "1"},
        "two stepped wildcards":                                      {"*/2", "*/2"},
    }

    for name, pair := range divergent {
        if false == busyboxDayFieldsDiverge(pair[0], pair[1]) {
            t.Fatalf("%s: expected the pair (%q, %q) to diverge", name, pair[0], pair[1])
        }
    }
}

func TestBusyboxDayFieldsDiverge_PassesThePairsEveryTargetReadsAlike(t *testing.T) {
    agreeing := map[string][2]string{
        "both plain wildcards":                            {"*", "*"},
        "restricted day of month beside a plain wildcard": {"15", "*"},
        "restricted weekday beside a plain wildcard":      {"*", "1"},
        "two restricted partial fields":                   {"15", "1"},
        "stepped wildcard beside a plain wildcard":        {"*/2", "*"},
        "full-coverage weekday beside a plain wildcard":   {"*", "0-6"},
    }

    for name, pair := range agreeing {
        if true == busyboxDayFieldsDiverge(pair[0], pair[1]) {
            t.Fatalf("%s: expected the pair (%q, %q) to agree", name, pair[0], pair[1])
        }
    }
}

/* a spelling that does not parse is left to validateScheduleFields, whose refusal names the field; answering divergent here would bury that message under the busybox one */
func TestBusyboxDayFieldsDiverge_LeavesAMalformedFieldToTheScheduleValidation(t *testing.T) {
    if true == busyboxDayFieldsDiverge("not-a-field", "1") {
        t.Fatal("a malformed day of month must not be reported as busybox divergence")
    }

    if true == busyboxDayFieldsDiverge("1", "not-a-field") {
        t.Fatal("a malformed day of week must not be reported as busybox divergence")
    }
}
