package static

import (
    "errors"
    "io/fs"
    "os"
    "path/filepath"
    "runtime"
    "testing"
)

func TestDirFileSystem_OpenRootReturnsDirectory(t *testing.T) {
    directory := t.TempDir()

    fileSystem := osDirFileSystem(directory)

    file, err := fileSystem.Open("")
    if nil != err {
        t.Fatalf("open root error: %v", err)
    }
    defer file.Close()

    info, err := file.Stat()
    if nil != err {
        t.Fatalf("stat error: %v", err)
    }

    if false == info.IsDir() {
        t.Fatalf("expected directory")
    }
}

func TestDirFileSystem_OpenPathWithinRootSucceeds(t *testing.T) {
    directory := t.TempDir()

    filePath := filepath.Join(directory, "file.txt")
    writeErr := os.WriteFile(filePath, []byte("hello"), 0o644)
    if nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }

    fileSystem := osDirFileSystem(directory)

    file, err := fileSystem.Open("file.txt")
    if nil != err {
        t.Fatalf("open error: %v", err)
    }
    defer file.Close()
}

func TestDirFileSystem_OpenAbsolutePathRejected(t *testing.T) {
    directory := t.TempDir()

    fileSystem := osDirFileSystem(directory)

    _, err := fileSystem.Open("/etc/passwd")
    if false == errors.Is(err, fs.ErrInvalid) {
        t.Fatalf("expected fs.ErrInvalid, got %v", err)
    }
}

func TestDirFileSystem_OpenParentTraversalRejected(t *testing.T) {
    directory := t.TempDir()

    fileSystem := osDirFileSystem(directory)

    _, err := fileSystem.Open("..")
    if false == errors.Is(err, fs.ErrPermission) {
        t.Fatalf("expected fs.ErrPermission, got %v", err)
    }

    _, err = fileSystem.Open("../secret.txt")
    if false == errors.Is(err, fs.ErrPermission) {
        t.Fatalf("expected fs.ErrPermission, got %v", err)
    }
}

func TestDirFileSystem_OpenSymlinkEscapingRootRejected(t *testing.T) {
    if "windows" == runtime.GOOS {
        t.Skip("symlink behavior differs on windows")
    }

    outsideDir := t.TempDir()
    outsideFile := filepath.Join(outsideDir, "secret.txt")
    writeErr := os.WriteFile(outsideFile, []byte("secret"), 0o644)
    if nil != writeErr {
        t.Fatalf("write outside error: %v", writeErr)
    }

    rootDir := t.TempDir()
    linkPath := filepath.Join(rootDir, "link.txt")
    if linkErr := os.Symlink(outsideFile, linkPath); nil != linkErr {
        t.Fatalf("symlink error: %v", linkErr)
    }

    fileSystem := osDirFileSystem(rootDir)

    _, err := fileSystem.Open("link.txt")
    if false == errors.Is(err, fs.ErrPermission) {
        t.Fatalf("expected fs.ErrPermission, got %v", err)
    }
}

func TestDirFileSystem_OpenSymlinkWithinRootAllowed(t *testing.T) {
    if "windows" == runtime.GOOS {
        t.Skip("symlink behavior differs on windows")
    }

    rootDir := t.TempDir()

    targetPath := filepath.Join(rootDir, "target.txt")
    if writeErr := os.WriteFile(targetPath, []byte("data"), 0o644); nil != writeErr {
        t.Fatalf("write target error: %v", writeErr)
    }

    linkPath := filepath.Join(rootDir, "link.txt")
    if linkErr := os.Symlink(targetPath, linkPath); nil != linkErr {
        t.Fatalf("symlink error: %v", linkErr)
    }

    fileSystem := osDirFileSystem(rootDir)

    file, err := fileSystem.Open("link.txt")
    if nil != err {
        t.Fatalf("open error: %v", err)
    }
    defer file.Close()
}

func TestDirFileSystem_OpenNonExistentPathReturnsError(t *testing.T) {
    directory := t.TempDir()

    fileSystem := osDirFileSystem(directory)

    _, err := fileSystem.Open("does-not-exist.txt")
    if nil == err {
        t.Fatalf("expected error")
    }
}

/* the padded and the exact spelling name two different files, and only the exact one exists: collapsing them would resolve a request the rules in front of the application judged on the padded spelling */
func TestDirFileSystem_OpenDoesNotTrimTheName(t *testing.T) {
    directory := t.TempDir()

    filePath := filepath.Join(directory, "file.txt")
    if writeErr := os.WriteFile(filePath, []byte("hello"), 0o644); nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }

    fileSystem := osDirFileSystem(directory)

    for _, name := range []string{
        " file.txt",
        "file.txt ",
        "\tfile.txt",
    } {
        file, err := fileSystem.Open(name)
        if nil == err {
            _ = file.Close()

            t.Fatalf("expected %q not to resolve to the untrimmed name", name)
        }
    }

    file, err := fileSystem.Open("file.txt")
    if nil != err {
        t.Fatalf("open error: %v", err)
    }
    defer file.Close()
}

func TestHasExcludedPathPrefix_MatchesTheRawRequestPathLikeThePrefixMatcher(t *testing.T) {
    excludedPathList := []string{"/admin", "/api/internal"}

    excluded := []string{
        "/admin",
        "/admin/",
        "/admin/users.json",
        "/administration/report.csv",
        "/api/internal/health",
    }

    for _, requestPath := range excluded {
        if false == hasExcludedPathPrefix(requestPath, excludedPathList) {
            t.Fatalf("expected %q to be excluded", requestPath)
        }
    }

    retained := []string{
        "/",
        "/index.html",
        "/api/public/health",
        "/assets/admin.css",
    }

    for _, requestPath := range retained {
        if true == hasExcludedPathPrefix(requestPath, excludedPathList) {
            t.Fatalf("expected %q not to be excluded", requestPath)
        }
    }
}

func TestHasExcludedPathPrefix_EmptyListExcludesNothing(t *testing.T) {
    if true == hasExcludedPathPrefix("/index.html", []string{}) {
        t.Fatalf("expected an empty list to exclude nothing")
    }

    if true == hasExcludedPathPrefix("/index.html", nil) {
        t.Fatalf("expected a nil list to exclude nothing")
    }
}

func TestHasExcludedPathPrefix_EmptyEntryExcludesEverything(t *testing.T) {
    /* the prefix comparison is the one security.NewPathPrefixMatcher makes, where an empty prefix matches every path; the configuration refuses such an entry precisely because it reaches this outcome. */
    if false == hasExcludedPathPrefix("/index.html", []string{""}) {
        t.Fatalf("expected an empty entry to exclude every path")
    }
}

/* the substitution of "." for the empty name is inert for the outcome — joining "." onto the base resolves back to the base — so this test does not prove it on position; the inversion does, by emptying every name that is not "." and answering the root for a request that named a file. */
func TestDirFileSystem_OpenDotNamesTheServedDirectory(t *testing.T) {
    directory := t.TempDir()

    fileSystem := osDirFileSystem(directory)

    file, err := fileSystem.Open(".")
    if nil != err {
        t.Fatalf("open dot error: %v", err)
    }
    defer file.Close()

    info, statErr := file.Stat()
    if nil != statErr {
        t.Fatalf("stat error: %v", statErr)
    }

    if false == info.IsDir() {
        t.Fatalf("expected \".\" to name the served directory")
    }
}

/* the fallback that keeps the configured base when the base itself does not resolve is LATENT: resolving the target resolves the base as its prefix, so the base cannot fail while the target succeeds, and the target's own failure is answered above it — which is what this asserts. Only the base disappearing between the two resolutions could enter that branch, which no in-process state forces. */
func TestDirFileSystem_ABaseThatDoesNotResolveIsRefusedOnTheTargetFirst(t *testing.T) {
    directory := t.TempDir()

    fileSystem := osDirFileSystem(filepath.Join(directory, "absent"))

    _, err := fileSystem.Open("a.txt")
    if nil == err {
        t.Fatalf("expected a base that does not resolve to refuse the target")
    }

    if true == errors.Is(err, fs.ErrPermission) {
        t.Fatalf("expected the target's own resolution failure rather than the containment refusal, got %v", err)
    }
}

/* the mirror of the firewall matcher's trailing-slash reading: an entry written "/admin/" claims the bare "/admin" too, and nothing wider — the two comparisons promise to select the same requests */
func TestHasExcludedPathPrefix_ATrailingSlashEntryClaimsTheBareSpelling(t *testing.T) {
    excludedPathList := []string{"/admin/"}

    excluded := []string{"/admin", "/admin/", "/admin/users.json"}
    for _, requestPath := range excluded {
        if false == hasExcludedPathPrefix(requestPath, excludedPathList) {
            t.Fatalf("expected %q to be excluded by the trailing-slash entry", requestPath)
        }
    }

    retained := []string{"/administrator", "/admi", "/"}
    for _, requestPath := range retained {
        if true == hasExcludedPathPrefix(requestPath, excludedPathList) {
            t.Fatalf("expected %q not to be excluded by the trailing-slash entry", requestPath)
        }
    }
}

/* "." resolves every name to a relative path that carries no "./" prefix, and "/" joins with the separator into "//", so a containment read as a textual prefix of the base refuses every name under both */
func TestDirFileSystem_OpenServesANameUnderTheCurrentDirectoryAndUnderTheFilesystemRoot(t *testing.T) {
    directory := t.TempDir()

    workingDirectory, getwdErr := os.Getwd()
    if nil != getwdErr {
        t.Fatalf("getwd error: %v", getwdErr)
    }
    if chdirErr := os.Chdir(directory); nil != chdirErr {
        t.Fatalf("chdir error: %v", chdirErr)
    }
    t.Cleanup(func() {
        _ = os.Chdir(workingDirectory)
    })

    if writeErr := os.WriteFile("file.txt", []byte("hello"), 0o644); nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }

    realDirectory, evalErr := filepath.EvalSymlinks(directory)
    if nil != evalErr {
        t.Fatalf("eval error: %v", evalErr)
    }

    filesystemRoot := filepath.VolumeName(realDirectory) + string(os.PathSeparator)
    nameUnderFilesystemRoot, relativeErr := filepath.Rel(filesystemRoot, filepath.Join(realDirectory, "file.txt"))
    if nil != relativeErr {
        t.Fatalf("rel error: %v", relativeErr)
    }

    cases := []struct {
        base string
        name string
    }{
        {".", "file.txt"},
        {filesystemRoot, filepath.ToSlash(nameUnderFilesystemRoot)},
    }
    for _, testCase := range cases {
        file, openErr := osDirFileSystem(testCase.base).Open(testCase.name)
        if nil != openErr {
            t.Fatalf("expected %q under the base %q to open, got %v", testCase.name, testCase.base, openErr)
        }
        _ = file.Close()
    }

    outsideDirectory := t.TempDir()
    if writeErr := os.WriteFile(filepath.Join(outsideDirectory, "secret.txt"), []byte("secret"), 0o644); nil != writeErr {
        t.Fatalf("write error: %v", writeErr)
    }
    if symlinkErr := os.Symlink(filepath.Join(outsideDirectory, "secret.txt"), "innocent.txt"); nil != symlinkErr {
        t.Fatalf("symlink error: %v", symlinkErr)
    }

    _, refuseErr := osDirFileSystem(".").Open("innocent.txt")
    if false == errors.Is(refuseErr, fs.ErrPermission) {
        t.Fatalf("expected a symlink escaping the current directory to be refused by the containment, got %v", refuseErr)
    }
}
