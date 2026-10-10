package internal

import (
    "strings"
    "testing"
)

func TestValidateTrustedProxyList_AcceptsPrefixesAddressesAndEmptyEntries(t *testing.T) {
    validationErr := ValidateTrustedProxyList([]string{"10.0.0.0/8", "::ffff:10.0.0.5", "192.168.1.1", "  ", ""})
    if nil != validationErr {
        t.Fatalf("expected the list to be accepted, got %v", validationErr)
    }
}

func TestValidateTrustedProxyList_RefusesAnEntryThatIsNeitherPrefixNorAddress(t *testing.T) {
    validationErr := ValidateTrustedProxyList([]string{"10.0.0.0/8", "10.0.0.0/33"})
    if nil == validationErr {
        t.Fatalf("expected the malformed prefix to be refused")
    }

    /* the entry is named so the operator can find the typo without bisecting the configuration */
    if false == strings.Contains(validationErr.Error(), "neither a CIDR prefix nor an address") {
        t.Fatalf("unexpected refusal: %v", validationErr)
    }
}

func TestValidateTrustedProxyList_OfNothingIsAccepted(t *testing.T) {
    if validationErr := ValidateTrustedProxyList(nil); nil != validationErr {
        t.Fatalf("expected an empty list to be accepted, got %v", validationErr)
    }
}
