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
