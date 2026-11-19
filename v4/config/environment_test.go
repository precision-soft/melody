package config

import (
    "context"
    "errors"
    "testing"
)

func TestNewEnvironmentPropagatesSourceError(t *testing.T) {
    source := &testEnvironmentSource{
        values: nil,
        err:    errors.New("load failed"),
    }

    _, err := NewEnvironment(context.Background(), source)
    if nil == err {
        t.Fatalf("expected error")
    }
}

func TestEmptyStringIsPresentValue(t *testing.T) {
    source := &testEnvironmentSource{
        values: map[string]string{
            LogPathKey: "",
        },
    }

    environment, err := NewEnvironment(context.Background(), source)
    if nil != err {
        t.Fatalf("new environment error: %v", err)
    }

    configuration, err := NewConfiguration(environment, "/tmp/melody")
    if nil != err {
        t.Fatalf("new configuration error: %v", err)
    }

    if "" != configuration.MustGet(KernelLogPath).String() {
        t.Fatalf("expected empty string to be present value")
    }
}

func TestEnvironment_TheDirectoryNamesAreSpelledOut(t *testing.T) {
    for constant, expected := range map[string]string{
        PublicDirectoryKey:     "MELODY_PUBLIC_DIRECTORY",
        KernelPublicDirectory:  "kernel.public_directory",
        KernelProjectDirectory: "kernel.project_directory",
        KernelLogsDirectory:    "kernel.logs_directory",
        KernelCacheDirectory:   "kernel.cache_directory",
    } {
        if expected != constant {
            t.Fatalf("expected %q, got %q", expected, constant)
        }
    }
}

type contextRecordingEnvironmentSource struct {
    seen context.Context
}

func (instance *contextRecordingEnvironmentSource) Load(ctx context.Context) (map[string]string, error) {
    instance.seen = ctx

    return map[string]string{}, nil
}

type contextKeyForEnvironment struct{}

func TestNewEnvironment_HandsItsContextToTheSourceAndRefusesNone(t *testing.T) {
    source := &contextRecordingEnvironmentSource{}
    ctx := context.WithValue(context.Background(), contextKeyForEnvironment{}, "marker")

    if _, err := NewEnvironment(ctx, source); nil != err || nil == source.seen || "marker" != source.seen.Value(contextKeyForEnvironment{}) {
        t.Fatalf("expected the source handed the context NewEnvironment was handed, got %v (%v)", source.seen, err)
    }

    //nolint:staticcheck // the nil context is the input under test
    if _, err := NewEnvironment(nil, source); nil == err {
        t.Fatalf("expected a nil context refused")
    }
}

func TestEnvironmentSource_ADoneContextIsRefusedBeforeAFileIsRead(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()

    if _, err := writeDotEnvFiles(t, map[string]string{".env": "APP_KEY=value\n"}).Load(ctx); false == errors.Is(err, context.Canceled) {
        t.Fatalf("expected the done context refused, got %v", err)
    }
}

func TestEnvironmentSource_ARefusesANilContext(t *testing.T) {
    //nolint:staticcheck // the nil context is the input under test
    if _, err := writeDotEnvFiles(t, map[string]string{".env": "APP_KEY=value\n"}).Load(nil); nil == err {
        t.Fatalf("expected a nil context refused")
    }
}
