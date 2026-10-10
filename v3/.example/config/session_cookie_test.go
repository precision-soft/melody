package config

import (
    nethttp "net/http"
    "testing"

    melodyexception "github.com/precision-soft/melody/v3/exception"
    melodyhttpcontract "github.com/precision-soft/melody/v3/http/contract"
)

type recordingSessionCookiePolicySetter struct {
    policyList []melodyhttpcontract.SessionCookiePolicy
}

func (instance *recordingSessionCookiePolicySetter) SetSessionCookiePolicy(policy melodyhttpcontract.SessionCookiePolicy) {
    instance.policyList = append(instance.policyList, policy)
}

func TestApplySessionCookieSecure_MarksTheCookieSecureOnTheSwitch(t *testing.T) {
    setter := &recordingSessionCookiePolicySetter{}
    applySessionCookieSecure(setter, "always")

    if 1 != len(setter.policyList) || melodyhttpcontract.SessionCookieSecureAlways != setter.policyList[0].Secure {
        t.Fatalf("expected the always policy installed once, got %v", setter.policyList)
    }

    if "/" != setter.policyList[0].Path || "" != setter.policyList[0].Domain || nethttp.SameSiteLaxMode != setter.policyList[0].SameSite {
        t.Fatalf("expected the kernel's default path, domain and SameSite=Lax restated, got %+v", setter.policyList[0])
    }

    for _, value := range []string{"", "from-scheme", " "} {
        untouched := &recordingSessionCookiePolicySetter{}
        applySessionCookieSecure(untouched, value)

        if 0 != len(untouched.policyList) {
            t.Fatalf("expected %q to leave the kernel's default, got %v", value, untouched.policyList)
        }
    }
}

func TestApplySessionCookieSecure_RefusesAnUnknownValueNamingTheKey(t *testing.T) {
    for _, value := range []string{"true", "Always", "never"} {
        refused := func() (recovered any) {
            defer func() {
                recovered = recover()
            }()

            applySessionCookieSecure(&recordingSessionCookiePolicySetter{}, value)

            return nil
        }()

        refusal, isError := refused.(error)
        if false == isError || "APP_SESSION_COOKIE_SECURE" != melodyexception.LogContext(refusal)["key"] {
            t.Fatalf("expected %q refused naming the key, got %v", value, refused)
        }
    }
}
