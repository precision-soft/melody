package logging

import (
    "regexp"
    "testing"
)

func TestGenerateProcessId_HasTheUuidVersionFourShape(t *testing.T) {
    pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

    for iteration := 0; iteration < 50; iteration++ {
        processId := GenerateProcessId()

        if false == pattern.MatchString(processId) {
            t.Fatalf("unexpected process id shape: %q", processId)
        }
    }
}

func TestGenerateProcessId_DoesNotRepeat(t *testing.T) {
    seenIdList := map[string]bool{}

    for iteration := 0; iteration < 200; iteration++ {
        processId := GenerateProcessId()

        if true == seenIdList[processId] {
            t.Fatalf("process id %q was generated twice", processId)
        }

        seenIdList[processId] = true
    }
}
