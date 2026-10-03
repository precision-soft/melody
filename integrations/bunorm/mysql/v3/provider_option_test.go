package mysql

import (
    "context"
    "testing"

    driver "github.com/go-sql-driver/mysql"
)

func TestWithPostBuildHook(t *testing.T) {
    hookCalled := false

    provider := &Provider{}
    WithPostBuildHook(func(ctx context.Context, driverConfig *driver.Config) error {
        hookCalled = true
        return nil
    })(provider)

    if nil == provider.postBuildHook {
        t.Fatalf("expected the option to install the hook")
    }

    _ = provider.postBuildHook(context.Background(), driver.NewConfig())
    if false == hookCalled {
        t.Fatalf("expected the installed hook to be the given one")
    }
}
