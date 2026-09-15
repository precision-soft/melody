package config

import (
    "testing"

    "github.com/precision-soft/melody/v3/.example/persistence"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodylock "github.com/precision-soft/melody/v3/lock"
    melodylockcontract "github.com/precision-soft/melody/v3/lock/contract"
)

func TestRegisterArchiveLockerService_HandsTheInProcessLockerWhenNoArchiveIsWired(t *testing.T) {
    moduleInstance := &Module{}
    serviceContainer := melodycontainer.NewContainer()

    moduleInstance.registerArchiveLockerService(&containerRegistrar{Container: serviceContainer})

    locker, resolveErr := melodycontainer.FromResolver[melodylockcontract.Locker](serviceContainer, persistence.ServiceArchiveLocker)
    if nil != resolveErr {
        t.Fatalf("expected the archive locker to be registered without an archive, got %v", resolveErr)
    }

    if _, inProcess := locker.(*melodylock.InMemoryLocker); false == inProcess {
        t.Fatalf("expected the in-process locker without an archive, got %T", locker)
    }
}
