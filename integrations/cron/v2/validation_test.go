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

/* @info crond treats one out-of-range field as a parse error and refuses the whole crontab file with it, so generation must fail on the same bounds the in-process matcher enforces */
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

/* @info day of week 7 is the Sunday alias vixie crond accepts */
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
