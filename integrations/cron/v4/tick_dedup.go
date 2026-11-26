package cron

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "strconv"
    "sync"
    "time"
    "unicode"

    "github.com/precision-soft/melody/v4/bound"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    "github.com/precision-soft/melody/v4/container/runtime"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/lock"
    lockcontract "github.com/precision-soft/melody/v4/lock/contract"
    "github.com/precision-soft/melody/v4/logging"
)

/* TickDedupConfig declares how the runner keeps one tick of an entry to one replica. Before it runs a due entry the runner acquires a lock named after the entry and the minute, and keeps it past the command's end, so a replica evaluating that minute later skips it. The zero value is the default: on, the application's locker, a five minute window, a tick not run when the locker cannot be reached. */
type TickDedupConfig struct {
    /* Disabled turns the dedup off for every entry: each replica runs every due entry, as a runner with no locker does. */
    Disabled bool

    /* Locker holds the markers: the application's service.lock.locker when left out. With neither, the runner deduplicates nothing and Run says so in one warning. A lease locker, the rueidis one, is the fit: a session locker, the mysql and pgsql ones, pins one connection per held tick and loses the marker with its process. A typed nil is refused. */
    Locker lockcontract.Locker

    /* KeyPrefix opens every marker's name, followed by the command name, a digest of the command name, the schedule and the arguments, and the tick's unix minute: the application namespace and ":cron:tick:" when left out, melody:cron:tick: under the default namespace. A prefix holding whitespace or a control character is refused. */
    KeyPrefix string

    /* Window is how far apart two replicas may evaluate one tick and still be deduplicated, the clock skew and the wake latency between them: 5m by default, at least 1s, unlimited refused, since a marker that never lapses is a lease no locker admits. A marker is held for it, and for it plus RunnerConfig.ClockJumpResetThreshold when the entry pins a minute and an hour, since such an entry is also evaluated in catch-up. */
    Window bound.Duration

    /* FailureMode is what a due entry does when the locker cannot be reached: TickDedupFailureSkip when left out, the tick not run and reported as that run's failure under ErrTickDedupUnavailable; TickDedupFailureRun runs it on every replica that could not ask, with a warning. Any other value is refused (ErrUnknownTickDedupFailureMode). */
    FailureMode TickDedupFailureMode

    /* CallTimeout bounds each acquire and each release the runner issues: 5s by default, at least 1ms, unlimited refused, since a store that never answers would hold the tick's goroutine. An acquire it ends is a locker failure. */
    CallTimeout bound.Duration
}

/* EntryTickDedupConfig is what one entry declares for itself under the in-process runner's tick dedup; its zero value follows the runner, and the generated manifests ignore it. */
type EntryTickDedupConfig struct {
    /* Disabled runs this entry on every replica that evaluates its tick, whatever RunnerConfig.TickDedup says: the door for a job that is per-replica by nature, a local cache warm-up or a temporary directory sweep. */
    Disabled bool

    /* Window replaces RunnerConfig.TickDedup.Window for this entry: the runner's when left out, at least 1s, unlimited refused. */
    Window bound.Duration

    /* FailureMode replaces RunnerConfig.TickDedup.FailureMode for this entry: the runner's when left out. */
    FailureMode TickDedupFailureMode
}

/* TickDedupFailureMode selects what a due entry does when the tick dedup locker cannot be reached. */
type TickDedupFailureMode string

const (
    /* TickDedupFailureSkip does not run the tick and reports it as a failed run: an unreachable store must not run one tick on every replica. */
    TickDedupFailureSkip TickDedupFailureMode = "skip"

    /* TickDedupFailureRun runs the tick without the marker, on every replica that could not reach the locker. */
    TickDedupFailureRun TickDedupFailureMode = "run"
)

/* TickSkipReasonHeldByAnotherInstance is the reason a minute's document gives an entry it skipped because another instance holds its tick. */
const TickSkipReasonHeldByAnotherInstance = "the tick is held by another instance"

const tickDedupKeyPrefixInfix = ":cron:tick:"

var runnerTickDedupWindowRule = bound.Rule[time.Duration]{
    Name:    "RunnerConfig.TickDedup.Window",
    Default: 5 * time.Minute,
    Minimum: time.Second,
}

var runnerTickDedupCallTimeoutRule = bound.Rule[time.Duration]{
    Name:    "RunnerConfig.TickDedup.CallTimeout",
    Default: 5 * time.Second,
    Minimum: bound.TimeoutMinimum,
}

var entryTickDedupWindowRule = bound.Rule[time.Duration]{
    Name:    "EntryConfig.TickDedup.Window",
    Minimum: time.Second,
}

const (
    runnerTickDedupFailureModeField = "RunnerConfig.TickDedup.FailureMode"
    entryTickDedupFailureModeField  = "EntryConfig.TickDedup.FailureMode"
)

type resolvedTickDedup struct {
    disabled    bool
    locker      lockcontract.Locker
    keyPrefix   string
    window      time.Duration
    failureMode TickDedupFailureMode
    callTimeout time.Duration
}

func resolveTickDedupConfig(config TickDedupConfig) (resolvedTickDedup, error) {
    if nil != config.Locker && true == isNilInterface(config.Locker) {
        return resolvedTickDedup{}, exception.NewError(
            "cron runner tick dedup locker is nil",
            exceptioncontract.Context{"field": "RunnerConfig.TickDedup.Locker"},
            nil,
        )
    }

    for _, character := range config.KeyPrefix {
        if true == unicode.IsControl(character) || true == unicode.IsSpace(character) {
            return resolvedTickDedup{}, exception.NewError(
                "cron runner tick dedup key prefix holds whitespace or a control character",
                exceptioncontract.Context{
                    "field": "RunnerConfig.TickDedup.KeyPrefix",
                    "value": strconv.Quote(config.KeyPrefix),
                },
                nil,
            )
        }
    }

    window, windowErr := bound.Resolve(config.Window, runnerTickDedupWindowRule)
    if nil != windowErr {
        return resolvedTickDedup{}, windowErr
    }

    callTimeout, callTimeoutErr := bound.Resolve(config.CallTimeout, runnerTickDedupCallTimeoutRule)
    if nil != callTimeoutErr {
        return resolvedTickDedup{}, callTimeoutErr
    }

    failureMode, failureModeErr := resolveTickDedupFailureMode(config.FailureMode, TickDedupFailureSkip, runnerTickDedupFailureModeField)
    if nil != failureModeErr {
        return resolvedTickDedup{}, failureModeErr
    }

    return resolvedTickDedup{
        disabled:    config.Disabled,
        locker:      config.Locker,
        keyPrefix:   config.KeyPrefix,
        window:      window.Value,
        failureMode: failureMode,
        callTimeout: callTimeout.Value,
    }, nil
}

func resolveTickDedupFailureMode(mode TickDedupFailureMode, inherited TickDedupFailureMode, field string) (TickDedupFailureMode, error) {
    switch mode {
    case "":
        return inherited, nil
    case TickDedupFailureSkip, TickDedupFailureRun:
        return mode, nil
    default:
        return "", exception.NewError(
            ErrUnknownTickDedupFailureMode.Error(),
            exceptioncontract.Context{
                "field": field,
                "value": string(mode),
            },
            ErrUnknownTickDedupFailureMode,
        )
    }
}

type resolvedEntryTickDedup struct {
    disabled    bool
    hold        time.Duration
    failureMode TickDedupFailureMode
}

/* resolveEntryTickDedup resolves one entry's posture over the runner's: its window, plus the catch-up horizon for a fixed-time entry, which reconcileWallClock may evaluate up to the threshold late. */
func resolveEntryTickDedup(commandName string, config EntryTickDedupConfig, fixedTime bool, runner resolvedRunner) (resolvedEntryTickDedup, error) {
    window := runner.tickDedup.window

    if false == config.Window.IsAbsent() {
        entryWindow, entryWindowErr := bound.Resolve(config.Window, entryTickDedupWindowRule)
        if nil != entryWindowErr {
            return resolvedEntryTickDedup{}, withCommandName(entryWindowErr, commandName)
        }

        window = entryWindow.Value
    }

    failureMode, failureModeErr := resolveTickDedupFailureMode(config.FailureMode, runner.tickDedup.failureMode, entryTickDedupFailureModeField)
    if nil != failureModeErr {
        return resolvedEntryTickDedup{}, withCommandName(failureModeErr, commandName)
    }

    hold := window
    if true == fixedTime {
        hold = window + runner.clockJumpResetThreshold
    }

    return resolvedEntryTickDedup{
        disabled:    config.Disabled,
        hold:        hold,
        failureMode: failureMode,
    }, nil
}

/* entryIdentityDigest tells apart two entries scheduling one command under different schedules or arguments: each field is length-prefixed, so no separator inside a field can make two lists hash alike. */
func entryIdentityDigest(commandName string, schedule string, arguments []string) string {
    hash := sha256.New()

    for _, field := range append([]string{commandName, schedule}, arguments...) {
        _, _ = fmt.Fprintf(hash, "%d:%s", len(field), field)
    }

    return hex.EncodeToString(hash.Sum(nil))[:16]
}

/* tickDedupKey names the marker of one tick of one entry; the minute is the absolute unix minute, never the wall index, so replicas in one zone agree and the two instants of a repeated wall minute stay two ticks. */
func tickDedupKey(prefix string, entry *scheduledRunEntry, at time.Time) string {
    return prefix + entry.commandName + ":" + entry.identityDigest + ":" + strconv.FormatInt(at.Unix()/60, 10)
}

/* tickDedupRun is what one Run of the runner deduplicates against: the locker, or the failure resolving the registered one, and the key prefix under the run's namespace. A nil run deduplicates nothing. */
type tickDedupRun struct {
    locker    lockcontract.Locker
    lockerErr error
    keyPrefix string
}

/* resolveTickDedupRun finds the locker once per Run: the configured one, else service.lock.locker when the run's scope has it, else none. */
func (instance *RunnerCommand) resolveTickDedupRun(runtimeInstance containercontract.Runtime) *tickDedupRun {
    if true == instance.tickDedup.disabled {
        return nil
    }

    dedupRun := &tickDedupRun{locker: instance.tickDedup.locker}

    if nil == dedupRun.locker {
        scope := runtimeInstance.Scope()
        if true == isNilInterface(scope) || false == scope.Has(lock.ServiceLocker) {
            return nil
        }

        dedupRun.locker, dedupRun.lockerErr = runtime.FromRuntime[lockcontract.Locker](runtimeInstance, lock.ServiceLocker)
        if nil == dedupRun.lockerErr && true == isNilInterface(dedupRun.locker) {
            dedupRun.lockerErr = exception.NewError("the registered locker resolved to nil", exceptioncontract.Context{"serviceName": lock.ServiceLocker}, nil)
        }
    }

    dedupRun.keyPrefix = instance.tickDedup.keyPrefix
    if "" == dedupRun.keyPrefix {
        dedupRun.keyPrefix = namespaceOf(runtimeInstance) + tickDedupKeyPrefixInfix
    }

    return dedupRun
}

/* namespaceOf reads the application namespace from the run's configuration, the default one when the run has none. */
func namespaceOf(runtimeInstance containercontract.Runtime) string {
    configuration, configurationErr := melodyconfig.ConfigFromRuntime(runtimeInstance)
    if nil != configurationErr || true == isNilInterface(configuration) {
        return melodyconfig.DefaultNamespace
    }

    kernelConfiguration := configuration.Kernel()
    if true == isNilInterface(kernelConfiguration) || "" == kernelConfiguration.Namespace() {
        return melodyconfig.DefaultNamespace
    }

    return kernelConfiguration.Namespace()
}

/* currentTickDedupRun answers the run Run resolved, else resolves one for a caller driving the dispatch directly. */
func (instance *RunnerCommand) currentTickDedupRun(runtimeInstance containercontract.Runtime) *tickDedupRun {
    if stored := instance.tickDedupRun.Load(); nil != stored {
        return stored
    }

    return instance.resolveTickDedupRun(runtimeInstance)
}

/* hasDedupedEntry answers whether any entry takes part in the dedup, so a runner whose every entry opted out writes no warning. */
func (instance *RunnerCommand) hasDedupedEntry() bool {
    for _, entry := range instance.entries {
        if false == entry.tickDedup.disabled {
            return true
        }
    }

    return false
}

/* warnWithoutTickDedupLocker writes the one warning of a runner that deduplicates nothing because it has no locker. */
func (instance *RunnerCommand) warnWithoutTickDedupLocker(runtimeInstance containercontract.Runtime, dedupRun *tickDedupRun) {
    if nil != dedupRun || true == instance.tickDedup.disabled || false == instance.hasDedupedEntry() {
        return
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Warning(
            "cron runner has no tick dedup locker: on several replicas every tick runs once per replica; name RunnerConfig.TickDedup.Locker, register service.lock.locker, or set RunnerConfig.TickDedup.Disabled",
            map[string]any{"serviceName": lock.ServiceLocker},
        )
    }
}

type tickVerdict int

const (
    tickVerdictRun tickVerdict = iota
    tickVerdictSkip
    tickVerdictFailed
    tickVerdictCancelled
)

/* tickClaim is one key's verdict inside one evaluated minute, resolved once by whichever entry asks first, so two entries of one identity get one verdict. */
type tickClaim struct {
    key     string
    tick    time.Time
    hold    time.Duration
    once    sync.Once
    verdict tickVerdict
    cause   error
}

func (instance *RunnerCommand) resolveClaim(runtimeInstance containercontract.Runtime, dedupRun *tickDedupRun, claim *tickClaim) (tickVerdict, error) {
    claim.once.Do(func() {
        claim.verdict, claim.cause = instance.claimTick(runtimeInstance, dedupRun, claim)
    })

    return claim.verdict, claim.cause
}

func (instance *RunnerCommand) claimTick(runtimeInstance containercontract.Runtime, dedupRun *tickDedupRun, claim *tickClaim) (verdict tickVerdict, cause error) {
    if true == instance.wonTicks.holds(claim.key) {
        return tickVerdictRun, nil
    }

    if nil != dedupRun.lockerErr {
        return tickVerdictFailed, dedupRun.lockerErr
    }

    defer func() {
        if recovered := recover(); nil != recovered {
            verdict = tickVerdictFailed
            cause = exception.NewError(
                "cron: the tick dedup locker panicked",
                exceptioncontract.Context{"panicValue": fmt.Sprintf("%v", recovered)},
                exception.PanicCause(recovered),
            )
        }
    }()

    tickLock, createErr := lock.CreateLock(dedupRun.locker, claim.key, bound.Of(claim.hold))
    if nil != createErr {
        return tickVerdictFailed, createErr
    }

    acquireContext, cancel := context.WithTimeout(runtimeInstance.Context(), instance.tickDedup.callTimeout)
    defer cancel()

    acquired, acquireErr := tickLock.Acquire(runtime.WithContext(runtimeInstance, acquireContext))
    if nil != acquireErr {
        /* an acquire the stop ended is the stop, not an outage */
        if nil != runtimeInstance.Context().Err() {
            return tickVerdictCancelled, acquireErr
        }

        return tickVerdictFailed, acquireErr
    }

    if false == acquired {
        return tickVerdictSkip, nil
    }

    instance.wonTicks.record(claim.key, claim.tick, tickLock, claim.hold)

    return tickVerdictRun, nil
}

/* tickDedupUnavailableFailure is the single-valued link that answers errors.Is against ErrTickDedupUnavailable and keeps the locker's own cause on one chain, the shape commandTimeoutFailure has. */
type tickDedupUnavailableFailure struct {
    cause error
}

func (instance *tickDedupUnavailableFailure) Error() string {
    return ErrTickDedupUnavailable.Error()
}

func (instance *tickDedupUnavailableFailure) Is(target error) bool {
    return ErrTickDedupUnavailable == target
}

func (instance *tickDedupUnavailableFailure) Unwrap() error {
    return instance.cause
}

func newTickDedupUnavailableError(entry *scheduledRunEntry, claim *tickClaim, cause error) error {
    return exception.NewError(
        "cron: the tick dedup locker failed, the tick was not run",
        exceptioncontract.Context{
            "commandName":  entry.commandName,
            "tickDedupKey": claim.key,
            "tick":         claim.tick.Format(time.RFC3339),
        },
        &tickDedupUnavailableFailure{cause: cause},
    )
}

/* wonTick is one tick this runner won: its marker is released once its hold has elapsed, and the row is kept until the wall clock is also past the tick's hold, so a step back still finds it. */
type wonTick struct {
    tick     time.Time
    lock     lockcontract.Lock
    wonAt    time.Time
    hold     time.Duration
    released bool
}

/* wonTickLedger is what makes a runner never refuse itself: a tick it won runs again on a re-evaluation without asking the locker. */
type wonTickLedger struct {
    mutex sync.Mutex
    ticks map[string]*wonTick
}

func newWonTickLedger() *wonTickLedger {
    return &wonTickLedger{ticks: make(map[string]*wonTick)}
}

func (instance *wonTickLedger) holds(key string) bool {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    _, held := instance.ticks[key]

    return held
}

/* record dates the hold from after the acquire returned, so the release never precedes the store's own expiry. */
func (instance *wonTickLedger) record(key string, tick time.Time, tickLock lockcontract.Lock, hold time.Duration) {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.ticks[key] = &wonTick{tick: tick, lock: tickLock, wonAt: time.Now(), hold: hold}
}

func (instance *wonTickLedger) length() int {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return len(instance.ticks)
}

/* sweepWonTicks, handed the wake's own clock reading, releases every won tick whose hold has elapsed, once, and drops the rows the wall clock has also passed. A stopping runner releases nothing: a lease must outlive the process, or a rolling deploy reopens every tick it won. */
func (instance *RunnerCommand) sweepWonTicks(runtimeInstance containercontract.Runtime, wallNow time.Time) {
    if nil != runtimeInstance.Context().Err() {
        return
    }

    instance.wonTicks.mutex.Lock()
    due := make(map[string]*wonTick)
    for key, won := range instance.wonTicks.ticks {
        if instance.elapsedSince(won.wonAt) < won.hold {
            continue
        }

        if false == won.released {
            won.released = true
            due[key] = won
        }

        if true == wallNow.After(won.tick.Add(won.hold)) {
            delete(instance.wonTicks.ticks, key)
        }
    }
    instance.wonTicks.mutex.Unlock()

    for key, won := range due {
        instance.releaseWonTick(runtimeInstance, key, won)
    }
}

func (instance *RunnerCommand) releaseWonTick(runtimeInstance containercontract.Runtime, key string, won *wonTick) {
    defer func() {
        _ = recover()
    }()

    releaseContext, cancel := context.WithTimeout(context.WithoutCancel(runtimeInstance.Context()), instance.tickDedup.callTimeout)
    defer cancel()

    releaseErr := won.lock.Release(runtime.WithContext(runtimeInstance, releaseContext))
    if nil == releaseErr {
        return
    }

    if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
        logger.Warning(
            "cron runner could not release a won tick; the marker lapses with its ttl",
            exception.LogContext(releaseErr, exception.LogContextConfig{Extra: exceptioncontract.Context{"tickDedupKey": key}}),
        )
    }
}

/* gateTick resolves an entry's claim and answers what replaces its run: a skip row, a failed or cancelled row, or nothing when the entry runs. A locker failure under TickDedupFailureRun runs the entry with a warning. */
func (instance *RunnerCommand) gateTick(
    runtimeInstance containercontract.Runtime,
    dedupRun *tickDedupRun,
    entry *scheduledRunEntry,
    claim *tickClaim,
) (skip *dueSkip, row *dueRun, failure error, counted bool, gated bool) {
    verdict, cause := instance.resolveClaim(runtimeInstance, dedupRun, claim)

    switch verdict {
    case tickVerdictRun:
        return nil, nil, nil, false, false
    case tickVerdictSkip:
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Debug(
                "cron runner skipped a tick another instance holds",
                map[string]any{"commandName": entry.commandName, "tickDedupKey": claim.key},
            )
        }

        return &dueSkip{
            Command:   entry.commandName,
            Schedule:  entry.schedule,
            Arguments: argumentsOrEmpty(entry.arguments),
            Reason:    TickSkipReasonHeldByAnotherInstance,
        }, nil, nil, false, true
    case tickVerdictCancelled:
        cancelledRow := newDueRun(entry, "", time.Now(), cause, true)

        return nil, &cancelledRow, nil, false, true
    }

    unavailable := newTickDedupUnavailableError(entry, claim, cause)

    if TickDedupFailureRun == entry.tickDedup.failureMode {
        if logger := logging.LoggerFromRuntime(runtimeInstance); nil != logger {
            logger.Warning(
                "cron runner could not reach the tick dedup locker and runs the tick without its marker",
                exception.LogContext(unavailable, exception.LogContextConfig{}),
            )
        }

        return nil, nil, nil, false, false
    }

    runId := logging.GenerateProcessId()
    failedRow := newDueRun(entry, runId, time.Now(), unavailable, false)
    counted = instance.reportRunOutcome(runtimeInstance, entry, runId, unavailable, false)

    return nil, &failedRow, unavailable, counted, true
}
