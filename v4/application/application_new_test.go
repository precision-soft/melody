package application

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/container"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/precision-soft/melody/v4/internal/testhelper"
)

func TestFindProjectRootStartingFrom_FindsGoMod(t *testing.T) {
    projectDirectory := t.TempDir()

    err := os.WriteFile(filepath.Join(projectDirectory, "go.mod"), []byte("module example.com/test\n"), 0o644)
    if nil != err {
        t.Fatalf("failed to create go.mod: %v", err)
    }

    subDirectory := filepath.Join(projectDirectory, "a", "b")
    err = os.MkdirAll(subDirectory, 0o755)
    if nil != err {
        t.Fatalf("failed to create sub directory: %v", err)
    }

    resolvedProjectDirectory, err := findProjectRootStartingFrom(subDirectory)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if projectDirectory != resolvedProjectDirectory {
        t.Fatalf("expected %q, got %q", projectDirectory, resolvedProjectDirectory)
    }
}

func TestFindProjectRootStartingFrom_ReturnsErrorWhenNotFound(t *testing.T) {
    directory := t.TempDir()

    resolvedProjectDirectory, err := findProjectRootStartingFrom(directory)
    if nil == err {
        t.Fatalf("expected error")
    }
    if "" != resolvedProjectDirectory {
        t.Fatalf("expected empty directory, got %q", resolvedProjectDirectory)
    }
}

func TestWorkingDirectoryHasEnvironmentFile_DetectsDotEnv(t *testing.T) {
    directory := t.TempDir()

    err := os.WriteFile(filepath.Join(directory, ".env"), []byte{}, 0o644)
    if nil != err {
        t.Fatalf("failed to create .env: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected true when .env is present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_DetectsDotEnvLocal(t *testing.T) {
    directory := t.TempDir()

    err := os.WriteFile(filepath.Join(directory, ".env.local"), []byte{}, 0o644)
    if nil != err {
        t.Fatalf("failed to create .env.local: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected true when .env.local is present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_ReturnsFalseWhenAbsent(t *testing.T) {
    directory := t.TempDir()

    if true == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected false when no .env files are present")
    }
}

func TestWorkingDirectoryHasEnvironmentFile_IgnoresDirectoryNamedDotEnv(t *testing.T) {
    directory := t.TempDir()

    err := os.Mkdir(filepath.Join(directory, ".env"), 0o755)
    if nil != err {
        t.Fatalf("failed to create .env directory: %v", err)
    }

    if true == workingDirectoryHasEnvironmentFile(directory) {
        t.Fatalf("expected false when .env is a directory")
    }
}

/* stat-ing through a regular file yields ENOTDIR, which is how the non-not-exist stat error is produced here without permission tricks. */
func TestWorkingDirectoryHasEnvironmentFile_TreatsUnprovableStatErrorAsPresent(t *testing.T) {
    directory := t.TempDir()

    occupiedPath := filepath.Join(directory, "occupied")
    err := os.WriteFile(occupiedPath, []byte("not a directory"), 0o600)
    if nil != err {
        t.Fatalf("failed to create file: %v", err)
    }

    if false == workingDirectoryHasEnvironmentFile(occupiedPath) {
        t.Fatalf("expected true when the stat error cannot prove absence")
    }
}

func TestIsGoRunExecutableDirectory_MatchesOnlyTheToolsTemporaryDirectories(t *testing.T) {
    matching := []string{
        "/tmp/go-build2932477933/b001/exe",
        "/var/folders/x1/T/go-build1/b001/exe",
        "/tmp/go-build/b001/exe",
        "/root/.cache/go-build/77/77f7a6c161b2fd7fcb2ed10b4c769bbb65331d8d32f44f46148489c0a64cd13c-d",
    }

    for _, directory := range matching {
        if false == isGoRunExecutableDirectory(directory) {
            t.Fatalf("expected %q to be recognized as a go run build directory", directory)
        }
    }

    nonMatching := []string{
        "/opt/go-builder/bin",
        "/srv/go-build-artifacts/app",
        "/data/go-builds/bin",
        "/usr/local/bin",
    }

    for _, directory := range nonMatching {
        if true == isGoRunExecutableDirectory(directory) {
            t.Fatalf("expected %q not to be classified as a go run build directory", directory)
        }
    }
}

/* the refusal of an executable path whose symlinks cannot be followed carries the path handed in, since the resolved one is empty on a failure. */
func TestResolveExecutablePath_RefusalCarriesThePathAsHanded(t *testing.T) {
    missingPath := filepath.Join(t.TempDir(), "missing", "binary")

    _, resolveErr := resolveExecutablePath(missingPath)
    melodyErr, isMelodyErr := resolveErr.(*exception.Error)
    if false == isMelodyErr || "failed to resolve executable path" != melodyErr.Message() {
        t.Fatalf("expected the resolution to be refused, got %v", resolveErr)
    }

    if missingPath != melodyErr.Context()["executablePath"] {
        t.Fatalf("expected the refusal to carry the path as handed, got %v", melodyErr.Context())
    }
}

type creationWaitersProbe struct{}

func TestNewApplication_TheContainerCreationWaitersFieldReachesTheContainer(t *testing.T) {
    applicationInstance := NewApplication(context.Background(), ApplicationConfig{EnvFiles: testhelper.NewEmbeddedEnvFs(), PublicFiles: testhelper.NewEmbeddedStaticFs(), ContainerCreationWaiters: bound.Of(1)})
    serviceContainer := applicationInstance.kernel.ServiceContainer()
    defer func() {
        _ = serviceContainer.Close()
    }()

    providerEntered := make(chan struct{})
    releaseProvider := make(chan struct{})

    container.MustRegister[*creationWaitersProbe](serviceContainer, "app.creation.waiters", func(resolver containercontract.Resolver) (*creationWaitersProbe, error) {
        close(providerEntered)
        <-releaseProvider

        return &creationWaitersProbe{}, nil
    }, container.RegisterOptions{})

    results := make(chan error, 3)
    go func() {
        _, getErr := serviceContainer.Get(t.Context(), "app.creation.waiters")
        results <- getErr
    }()

    <-providerEntered

    for waiter := 0; waiter < 2; waiter++ {
        go func() {
            _, getErr := serviceContainer.Get(t.Context(), "app.creation.waiters")
            results <- getErr
        }()
    }

    select {
    case getErr := <-results:
        if false == errors.Is(getErr, container.ErrCreationWaitersExceeded) {
            t.Fatalf("expected the waiter past the bound of 1 to be refused, got %v", getErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatalf("expected the bound the application declared to refuse the second waiter")
    }

    close(releaseProvider)

    for answer := 0; answer < 2; answer++ {
        if getErr := <-results; nil != getErr {
            t.Fatalf("unexpected resolution error: %v", getErr)
        }
    }
}
