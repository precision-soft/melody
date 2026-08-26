package main

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "strings"
    "time"
)

/* exampleHmacEnvelopeLifetime mirrors defaultHmacSignerTtl in v3/security/hmac_signer.go: an envelope minted by internal:sign is valid for thirty seconds. It is the reason this file exists at all — see below. */
const exampleHmacEnvelopeLifetime = 30 * time.Second

/* exampleMintWorkspaceName is the workspace the mint binary is built into, under the temporary directory and therefore outside the repository bind mount: the file watcher that supervises the v3 example on :8080 never sees it and never restarts the application mid-section. */
const exampleMintWorkspaceName = "melody-e2e-example-mint"

/* the credential-minting sections drive the v3 example's own commands (auth:token, internal:sign, totp:code) to produce the exact credentials the running application accepts, so the harness never has to hold a copy of a demo secret. The binary is built ONCE and re-executed for each mint, and that is load-bearing rather than an optimisation:

   - internal:sign produces an envelope that expires after exampleHmacEnvelopeLifetime (thirty seconds);
   - `go run .` on a cold build cache compiles the whole framework, which takes minutes.

   Mint through `go run` and the envelope is stale before the http call is issued, so the firewall answers 401 and the section blames the code under test for a build delay. Building once moves the compile out of every mint, and runExampleMintCommand still returns the elapsed time so a caller can say which of the two happened.

   The workspace mirrors prepareExampleWorkspace (example.go) with one deliberate difference: NO .env.local is written. That file exists there only to move the http address off an occupied port, and a command binds no port — writing one here would hand the mint a configuration the serving application does not share. */
var exampleMintWorkspacePath string

var exampleMintBuildDuration time.Duration

var exampleMintReleaseFailureCleanup func()

/* exampleMintBinary builds the v3 example on first use and returns the path to the built binary. It is lazy so a run whose sections are all skipped never pays for the build. */
func exampleMintBinary() string {
    if "" != exampleMintWorkspacePath {
        return filepath.Join(exampleMintWorkspacePath, "application")
    }

    major, exists := exampleMajorByNumber(3)
    if false == exists {
        fail("example mint: melody has no major 3, so the v3 example commands cannot be built")
    }

    workspace := filepath.Join(os.TempDir(), exampleMintWorkspaceName)

    if removeErr := os.RemoveAll(workspace); nil != removeErr {
        fail("example mint: clear the workspace %s: %v", workspace, removeErr)
    }
    if makeErr := os.MkdirAll(workspace, 0o755); nil != makeErr {
        fail("example mint: create the workspace %s: %v", workspace, makeErr)
    }

    exampleMintReleaseFailureCleanup = pushFailureCleanup(func() {
        _ = os.RemoveAll(workspace)
    })

    directory := major.directory()

    startedAt := time.Now()

    build := exec.Command("go", "build", "-o", filepath.Join(workspace, "application"), ".")
    build.Dir = directory
    build.Env = exampleBuildEnvironment()

    if buildOutput, buildErr := build.CombinedOutput(); nil != buildErr {
        fail("example mint: build %s: %v\n%s", directory, buildErr, buildOutput)
    }

    exampleMintBuildDuration = time.Since(startedAt)

    /* melody derives the project directory from the location of the EXECUTABLE, so the .env has to sit beside the binary or every mint boots with no configuration at all and fails resolving the first parameter */
    environmentFile, readErr := os.ReadFile(filepath.Join(directory, ".env"))
    if nil != readErr {
        fail("example mint: read %s/.env: %v", directory, readErr)
    }
    if writeErr := os.WriteFile(filepath.Join(workspace, ".env"), environmentFile, 0o644); nil != writeErr {
        fail("example mint: write the workspace .env: %v", writeErr)
    }

    exampleMintWorkspacePath = workspace

    pass("built the v3 example command binary once for the credential mints (%s)", exampleMintBuildDuration.Round(time.Millisecond))

    return filepath.Join(workspace, "application")
}

/* releaseExampleMintWorkspace removes the mint workspace and unregisters its failure cleanup. The sections share one workspace, so it cannot be torn down by whichever section happens to finish first; main.go releases it once the whole example-over-http group is done. */
func releaseExampleMintWorkspace() {
    if "" == exampleMintWorkspacePath {
        return
    }

    if nil != exampleMintReleaseFailureCleanup {
        exampleMintReleaseFailureCleanup()
        exampleMintReleaseFailureCleanup = nil
    }

    _ = os.RemoveAll(exampleMintWorkspacePath)

    exampleMintWorkspacePath = ""
}

/* runExampleMintCommand executes one command of the built example and returns its ANSI-stripped output together with how long the execution took. The elapsed time is the second return value on purpose: an http call made with a credential whose mint took longer than the credential's own lifetime is not evidence about the firewall, and the caller uses exampleMintDelayDiagnostic to say so. */
func runExampleMintCommand(label string, arguments ...string) (string, time.Duration) {
    binary := exampleMintBinary()

    startedAt := time.Now()

    command := exec.Command(binary, arguments...)
    command.Dir = exampleMintWorkspacePath
    command.Env = exampleMintEnvironment()

    output, runErr := command.CombinedOutput()
    elapsed := time.Since(startedAt)

    plain := exampleStripAnsi(string(output))

    if nil != runErr {
        fail("%s: %s exited %v:\n%s", label, strings.Join(arguments, " "), runErr, exampleTail(plain, 20))
    }

    return plain, elapsed
}

/* exampleMintEnvironment withholds the harness's own backend variables from the mint, exactly as exampleRunEnvironment does for the per-major applications: melody resolves configuration from the .env beside the executable, and a leaked variable would reconfigure the mint away from the configuration the SERVING application runs under — a mint signing with a different secret than the firewall verifies with produces a 401 that looks like a security regression. */
func exampleMintEnvironment() []string {
    return exampleRunEnvironment()
}

/* exampleMintDelayDiagnostic returns the sentence a caller appends to a 401 diagnostic when the mint itself took long enough to explain the refusal. Empty when it did not, so a genuine refusal is never softened by a caveat that does not apply. This is the same class of diagnostic as stack.sh's holder-marker timeout: say the check could not be exercised rather than blame the property under test. */
func exampleMintDelayDiagnostic(elapsed time.Duration) string {
    if elapsed < exampleHmacEnvelopeLifetime {
        return ""
    }

    return fmt.Sprintf(
        " — the mint alone took %s, at or beyond the credential's %s lifetime, so the credential was already stale when it was presented: this is a build/boot delay, NOT a failure of the code under test",
        elapsed.Round(time.Millisecond),
        exampleHmacEnvelopeLifetime,
    )
}

/* exampleStripAnsi removes the cursor and colour sequences the command output frames its sections with, so the extraction below matches on the text a reader sees rather than on the escapes around it. */
func exampleStripAnsi(output string) string {
    return exampleAnsiPattern.ReplaceAllString(output, "")
}

/* exampleCompactTokenPattern matches the three dot-separated base64url segments both auth:token and internal:sign print. The anchors are what make the match safe: the commands frame their output with banners and print a json boot log above it, and a substring search would happily pick a fragment out of one of those lines. */
var exampleCompactTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

/* exampleMintedToken pulls the minted credential out of a command's output. The LAST matching line is taken, not the first: the boot log is printed above the command body, and a future log line that happened to look like a token would otherwise be handed to the firewall instead of the credential. */
func exampleMintedToken(label string, output string) string {
    token := exampleLastLineMatching(output, exampleCompactTokenPattern)
    if "" == token {
        fail("%s: the mint printed no compact token:\n%s", label, exampleTail(output, 20))
    }

    return token
}

var exampleTotpCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

func exampleMintedTotpCode(label string, output string) string {
    code := exampleLastLineMatching(output, exampleTotpCodePattern)
    if "" == code {
        fail("%s: totp:code printed no six-digit code:\n%s", label, exampleTail(output, 20))
    }

    return code
}

func exampleLastLineMatching(output string, pattern *regexp.Regexp) string {
    found := ""

    for _, line := range strings.Split(output, "\n") {
        trimmed := strings.TrimSpace(line)
        if true == pattern.MatchString(trimmed) {
            found = trimmed
        }
    }

    return found
}
