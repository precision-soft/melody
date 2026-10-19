package internal

import "testing"

func TestCarriesPercentEscapeOtherThanSeparator_ReadsOnlyAValidEscapeOtherThanTheSeparator(t *testing.T) {
    for declaredPath, expected := range map[string]bool{
        "/café":      false,
        "/a%2Fb":     false,
        "/a%2fb":     false,
        "/caf%C3%A9": true,
        "/a%20b":     true,
        "/a%2Fb%41":  true,
        "/100%":      false,
        "/a%2G":      false,
        "":           false,
    } {
        if expected != CarriesPercentEscapeOtherThanSeparator(declaredPath) {
            t.Fatalf("expected %q to answer %v", declaredPath, expected)
        }
    }
}
