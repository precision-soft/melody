package cron

import (
    "testing"
)

type capturingRegistrar struct {
    values map[string]any
}

func (instance *capturingRegistrar) RegisterParameter(name string, value any) {
    instance.values[name] = value
}

func TestRegisterDefaultParametersWiresExpectedDefaults(t *testing.T) {
    captured := &capturingRegistrar{values: map[string]any{}}

    RegisterDefaultParameters(captured)

    expected := map[string]string{
        ParameterDestinationFile: "%kernel.project_dir%/generated_conf/cron/crontab",
        ParameterLogsDir:         "%kernel.logs_dir%/cron",
        ParameterTemplate:        TemplateNameCrontab,
    }

    for name, want := range expected {
        got, ok := captured.values[name]
        if false == ok {
            t.Fatalf("RegisterDefaultParameters did not register %s", name)
        }
        if want != got {
            t.Fatalf("parameter %s = %v, want %q", name, got, want)
        }
    }

    if len(expected) != len(captured.values) {
        t.Fatalf("RegisterDefaultParameters registered %d parameters, want %d (%+v)", len(captured.values), len(expected), captured.values)
    }

    if _, registeredBinary := captured.values[ParameterBinary]; true == registeredBinary {
        t.Fatalf("RegisterDefaultParameters must not register %s; got %v", ParameterBinary, captured.values[ParameterBinary])
    }
    if _, registeredHeartbeat := captured.values[ParameterHeartbeatPath]; true == registeredHeartbeat {
        t.Fatalf("RegisterDefaultParameters must not register %s; got %v", ParameterHeartbeatPath, captured.values[ParameterHeartbeatPath])
    }
    if _, registeredUser := captured.values[ParameterUser]; true == registeredUser {
        t.Fatalf("RegisterDefaultParameters must not register %s; got %v", ParameterUser, captured.values[ParameterUser])
    }
    if _, registeredAutoHeartbeat := captured.values[ParameterHeartbeatAutoEnabled]; true == registeredAutoHeartbeat {
        t.Fatalf("RegisterDefaultParameters must not register %s; got %v", ParameterHeartbeatAutoEnabled, captured.values[ParameterHeartbeatAutoEnabled])
    }
}
