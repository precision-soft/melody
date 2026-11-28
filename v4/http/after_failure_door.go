package http

import (
    "slices"

    "github.com/precision-soft/melody/v4/internal"
    "github.com/precision-soft/melody/v4/internal/eventseam"
    loggingcontract "github.com/precision-soft/melody/v4/logging/contract"

    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

const (
    afterFailureWriteResponse = "response"
    afterFailureWriteRefusal  = "refusal"
)

/* the reasons the kernel refuses a write a listener's door admitted after a failure */
const (
    afterFailureRefusedByDoor                    = "door"
    afterFailureRefusedByRequiredListenerSkipped = "requiredListenerSkipped"
    afterFailureRefusedBySecondDecision          = "secondDecision"
    afterFailureRefusedByNoResponse              = "noResponse"
    afterFailureRefusedByExit                    = "exit"
)

/* afterFailureDoor is the one door a kernel payload's write passes when a listener makes it after a failure: the write stands only through the listener's ListenerConfig.Failure.ReplacesResponseAfterFailure, its attempt recorded either way for the kernel's warning */
type afterFailureDoor struct {
    /* the listener of the after-failure pass running now, nil outside that pass */
    afterFailureListener *eventseam.AfterFailureListener
    afterFailureWrites   []afterFailureWrite
}

/* afterFailureWrite is one write a listener made after a failure, admitted through its door or refused: the response standing when it wrote and what it wrote, a response or a refusal */
type afterFailureWrite struct {
    listener eventseam.AfterFailureListener
    write    string
    admitted bool
    standing httpcontract.Response
    written  httpcontract.Response
    refusal  error
}

/* EnterAfterFailureListener is the dispatcher's door: the listener named runs after a failure until the function answered is called. */
func (instance *afterFailureDoor) EnterAfterFailureListener(token eventseam.Token, listener eventseam.AfterFailureListener) func() {
    /* a token built outside v4 enters nothing, so no listener opens the door for itself */
    if false == token.Valid() {
        return func() {}
    }

    previous := instance.afterFailureListener
    instance.afterFailureListener = &listener

    return func() {
        instance.afterFailureListener = previous
    }
}

func (instance *afterFailureDoor) inAfterFailurePass() bool {
    return nil != instance.afterFailureListener
}

/* admitAfterFailureWrite answers whether a write may stand: always outside the after-failure pass, where nothing is recorded, and for a write that leaves the decision unchanged; inside the pass only through the listener's door, recorded either way */
func (instance *afterFailureDoor) admitAfterFailureWrite(write string, standing httpcontract.Response, written httpcontract.Response, refusal error, unchanged bool) bool {
    if nil == instance.afterFailureListener {
        return true
    }

    if true == unchanged {
        return true
    }

    admitted := instance.afterFailureListener.ReplacesResponse

    instance.afterFailureWrites = append(instance.afterFailureWrites, afterFailureWrite{
        listener: *instance.afterFailureListener,
        write:    write,
        admitted: admitted,
        standing: standing,
        written:  written,
        refusal:  refusal,
    })

    return admitted
}

/* takeAfterFailureWrites answers the writes recorded so far and forgets them, so each is warned about once */
func (instance *afterFailureDoor) takeAfterFailureWrites() []afterFailureWrite {
    writes := instance.afterFailureWrites
    instance.afterFailureWrites = nil

    return writes
}

func (instance *afterFailureDoor) hasAdmittedAfterFailureWrite() bool {
    return nil != firstAdmittedAfterFailureWrite(instance.afterFailureWrites)
}

/* firstAdmittedAfterFailureWrite answers the first write a door admitted, nil for none; its standing response is the one the failure left */
func firstAdmittedAfterFailureWrite(writes []afterFailureWrite) *afterFailureWrite {
    for index := range writes {
        if true == writes[index].admitted {
            return &writes[index]
        }
    }

    return nil
}

var _ eventseam.AfterFailureGuard = (*afterFailureDoor)(nil)

/* afterFailureVerdict is what the kernel decided once the dispatch returned */
type afterFailureVerdict struct {
    /* refusedBy is set when the kernel fails closed over the writes a door admitted */
    refusedBy string
    /* nilResponseStatus is the status reported for no response: 204 on kernel.response, 0 elsewhere, where the key is left out */
    nilResponseStatus int
    /* standingServed is set where a refused write leaves the standing response served, kernel.response; elsewhere the kernel answers for itself */
    standingServed bool
    /* held are the responses the caller still owns: served, or closed by the caller itself */
    held []httpcontract.Response
}

/* warnAfterFailureWrites writes one warning per write a listener made after a failure, refused or admitted, and closes the response each lost unless the caller holds it, answering what it closed; each response is closed once */
func warnAfterFailureWrites(logger loggingcontract.Logger, writes []afterFailureWrite, verdict afterFailureVerdict) []httpcontract.Response {
    closedResponses := make([]httpcontract.Response, 0)

    for _, write := range writes {
        refusedBy := ""
        if false == write.admitted {
            refusedBy = afterFailureRefusedByDoor
        } else if "" != verdict.refusedBy {
            refusedBy = verdict.refusedBy
        }

        logContext := loggingcontract.Context{
            "eventName":    write.listener.EventName,
            "listenerName": write.listener.ListenerName,
            "write":        write.write,
        }
        if nil != write.refusal {
            logContext["refusal"] = internal.BoundDiagnosticText(write.refusal.Error())
        }

        if "" == refusedBy {
            if afterFailureWriteResponse == write.write {
                logContext["servedResponseStatus"] = afterFailureStatusOf(write.written, verdict.nilResponseStatus)
            }
            addAfterFailureStatus(logContext, "replacedResponseStatus", write.standing, verdict.nilResponseStatus)

            logger.Warning("a listener replaced the response after a failure", logContext)
        } else {
            if afterFailureWriteResponse == write.write {
                logContext["refusedResponseStatus"] = afterFailureStatusOf(write.written, verdict.nilResponseStatus)
            }
            if afterFailureRefusedByDoor == refusedBy {
                standingKey := "standingResponseStatus"
                if true == verdict.standingServed {
                    standingKey = "servedResponseStatus"
                }
                addAfterFailureStatus(logContext, standingKey, write.standing, verdict.nilResponseStatus)
                logContext["door"] = "ListenerConfig.Failure.ReplacesResponseAfterFailure"
            }
            logContext["refusedBy"] = refusedBy

            logger.Warning("a listener's response replacement after a failure is refused", logContext)
        }

        /* an admitted write loses the response it replaced, a refused one its own, and one the kernel overruled both */
        discardedList := []httpcontract.Response{write.written}
        if true == write.admitted {
            discardedList = []httpcontract.Response{write.standing}
            if "" != refusedBy {
                discardedList = append(discardedList, write.written)
            }
        }

        for _, discarded := range discardedList {
            if true == internal.IsNilInterface(discarded) || true == slices.Contains(verdict.held, discarded) || true == slices.Contains(closedResponses, discarded) {
                continue
            }

            closeDiscardedResponseBody(discarded, logger)
            closedResponses = append(closedResponses, discarded)
        }
    }

    return closedResponses
}

/* afterFailureStatusOf answers the status of a response, nilResponseStatus for none */
func afterFailureStatusOf(response httpcontract.Response, nilResponseStatus int) int {
    if true == internal.IsNilInterface(response) {
        return nilResponseStatus
    }

    return response.StatusCode()
}

/* addAfterFailureStatus writes the status under key, leaving the key out for no response when the event reports none for it */
func addAfterFailureStatus(logContext loggingcontract.Context, key string, response httpcontract.Response, nilResponseStatus int) {
    if true == internal.IsNilInterface(response) && 0 == nilResponseStatus {
        return
    }

    logContext[key] = afterFailureStatusOf(response, nilResponseStatus)
}
