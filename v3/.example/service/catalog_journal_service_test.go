package service

import (
    "context"
    "testing"
    "time"

    "github.com/precision-soft/melody/v3/.example/repository"
    melodyapplication "github.com/precision-soft/melody/v3/application"
    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
    melodyruntimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    melodysecurity "github.com/precision-soft/melody/v3/security"
)

/* actorTestRuntime answers a runtime whose scope carries a console run's process context when processId is not empty, and a signed-in token when userIdentifier is not empty */
func actorTestRuntime(processId string, userIdentifier string) melodyruntimecontract.Runtime {
    containerInstance := melodycontainer.NewContainer()
    scope := containerInstance.NewScope()

    if "" != processId {
        scope.MustOverrideProtectedInstance(melodyapplication.ServiceProcessContext, melodyapplication.NewProcessContext(processId, time.Now()))
    }

    runtimeInstance := melodyruntime.New(context.Background(), scope, containerInstance)

    if "" != userIdentifier {
        firewall := melodysecurity.NewCompiledFirewall(
            "main",
            melodysecurity.NewPathPrefixMatcher("/"),
            "prefix /",
            nil,
            nil,
            nil,
            nil,
            nil,
            nil,
            nil,
            "",
            "",
            nil,
            nil,
            melodysecurity.SourceNone,
            melodysecurity.SourceNone,
            melodysecurity.SourceNone,
            melodysecurity.SourceNone,
            melodysecurity.SourceNone,
        )

        melodysecurity.SecurityContextSetOnRuntime(
            runtimeInstance,
            melodysecurity.NewSecurityContext(firewall, melodysecurity.NewAuthenticatedToken(userIdentifier, []string{"ROLE_USER"})),
        )
    }

    return runtimeInstance
}

func TestActorFromRuntime_NamesAConsoleRunByItsProcessId(t *testing.T) {
    actor := ActorFromRuntime(actorTestRuntime("4f0c2d9a-run", ""))

    if "process:4f0c2d9a-run" != actor {
        t.Fatalf("expected the console run named by its process id, got %q", actor)
    }
}

func TestActorFromRuntime_NamesTheSystemWithoutATokenOrAProcess(t *testing.T) {
    actor := ActorFromRuntime(actorTestRuntime("", ""))

    if repository.CatalogJournalActorSystem != actor {
        t.Fatalf("expected the system, got %q", actor)
    }
}

func TestActorFromRuntime_NamesTheSignedInUserAheadOfTheProcess(t *testing.T) {
    actor := ActorFromRuntime(actorTestRuntime("4f0c2d9a-run", "editor"))

    if "editor" != actor {
        t.Fatalf("expected the signed-in user, got %q", actor)
    }
}
