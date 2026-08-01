package serializer

import (
    "errors"
    "sort"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
    "github.com/precision-soft/melody/internal"
    serializercontract "github.com/precision-soft/melody/serializer/contract"
)

/* ErrNotAcceptable reports that the accept header refused every media type the manager can produce, so no representation can be served. */
var ErrNotAcceptable = errors.New("no acceptable media type for the accept header")

const notAcceptableMessage = "the accept header refuses every available media type"

func NewSerializerManager(serializersByMime map[string]serializercontract.Serializer) (*SerializerManager, error) {
    if nil == serializersByMime {
        serializersByMime = map[string]serializercontract.Serializer{}
    }

    normalizedSerializersByMime := make(map[string]serializercontract.Serializer, len(serializersByMime))
    rawKeysByNormalizedMime := make(map[string]string, len(serializersByMime))
    for mimeKey, serializerInstance := range serializersByMime {
        normalizedMimeKey := normalizeMime(mimeKey)
        if "" == normalizedMimeKey {
            return nil, exception.NewError(
                "serializer mime key is empty",
                exceptioncontract.Context{
                    "mime": mimeKey,
                },
                nil,
            )
        }

        /* @important a typed nil is refused alongside the untyped one: it passes the plain comparison, gets stored as a live serializer and dereferences its nil receiver on the first request the negotiation routes to it — the construction-time refusal exists precisely to keep that panic off the request path */
        if nil == serializerInstance || true == internal.IsNilInterface(serializerInstance) {
            return nil, exception.NewError(
                "serializer instance is nil",
                exceptioncontract.Context{
                    "mime": normalizedMimeKey,
                },
                nil,
            )
        }

        /* @important two spellings collapsing into one normalized key are refused instead of overwritten: map iteration order would decide the surviving serializer, so the winner would change from one boot to the next with the loser dropped silently */
        occupiedRawKey, occupied := rawKeysByNormalizedMime[normalizedMimeKey]
        if true == occupied {
            conflictingKeys := []string{occupiedRawKey, mimeKey}
            sort.Strings(conflictingKeys)

            return nil, exception.NewError(
                "serializer mime keys collide after normalization",
                exceptioncontract.Context{
                    "mime":         normalizedMimeKey,
                    "firstMimeKey": conflictingKeys[0],
                    "otherMimeKey": conflictingKeys[1],
                },
                nil,
            )
        }

        rawKeysByNormalizedMime[normalizedMimeKey] = mimeKey
        normalizedSerializersByMime[normalizedMimeKey] = serializerInstance
    }

    return &SerializerManager{
        serializersByMime: normalizedSerializersByMime,
    }, nil
}

type SerializerManager struct {
    serializersByMime map[string]serializercontract.Serializer
}

func (instance *SerializerManager) Get(mime string) (serializercontract.Serializer, bool) {
    normalizedMime := normalizeMime(mime)
    if "" == normalizedMime {
        return nil, false
    }

    serializerInstance, exists := instance.serializersByMime[normalizedMime]
    if false == exists {
        return nil, false
    }

    return serializerInstance, true
}

/* defaultSerializer answers the representation served when the header expresses no usable preference: the json serializer when one is registered, otherwise the first configured serializer in lexical mime order — an empty accept header means the client takes anything, so a manager deliberately configured without json serves what it has instead of refusing every request. */
func (instance *SerializerManager) defaultSerializer() (serializercontract.Serializer, bool) {
    serializerInstance, exists := instance.serializersByMime[MimeApplicationJson]
    if true == exists {
        return serializerInstance, true
    }

    configuredMimes := make([]string, 0, len(instance.serializersByMime))
    for configuredMime := range instance.serializersByMime {
        configuredMimes = append(configuredMimes, configuredMime)
    }

    if 0 == len(configuredMimes) {
        return nil, false
    }

    sort.Strings(configuredMimes)

    return instance.serializersByMime[configuredMimes[0]], true
}

func (instance *SerializerManager) ResolveByAcceptHeader(acceptHeader string) (serializercontract.Serializer, error) {
    acceptHeader = strings.TrimSpace(acceptHeader)
    if "" == acceptHeader {
        serializerInstance, exists := instance.defaultSerializer()
        if true == exists {
            return serializerInstance, nil
        }

        return nil, exception.NewError("no default serializer configured", nil, nil)
    }

    acceptedMimes := parseAcceptHeader(acceptHeader)
    if 0 == len(acceptedMimes) {
        return nil, exception.NewError(
            "no acceptable mime types in accept header",
            exceptioncontract.Context{"accept": acceptHeader},
            nil,
        )
    }

    candidateMimes := make([]string, 0, len(instance.serializersByMime))
    for candidateMime := range instance.serializersByMime {
        candidateMimes = append(candidateMimes, candidateMime)
    }

    sort.Strings(candidateMimes)

    /* @important each available type takes the quality of the MOST SPECIFIC range that covers it, so an exact range overrides a wildcard regardless of header order; a covered type whose range carries q=0 is refused rather than ignored, and a header that refuses every available type is answered as not acceptable instead of being served the very type it rejected */
    selectedMime := ""
    selectedQuality := 0.0
    selectedSpecificity := 0
    refusedEveryMatch := false

    for _, candidateMime := range candidateMimes {
        quality, specificity, matched := acceptQualityFor(acceptedMimes, candidateMime)
        if false == matched {
            continue
        }

        if 0 == quality {
            refusedEveryMatch = true

            continue
        }

        if quality > selectedQuality {
            selectedMime = candidateMime
            selectedQuality = quality
            selectedSpecificity = specificity

            continue
        }

        if quality == selectedQuality && specificity > selectedSpecificity {
            selectedMime = candidateMime
            selectedSpecificity = specificity
        }
    }

    if "" != selectedMime {
        return instance.serializersByMime[selectedMime], nil
    }

    if true == refusedEveryMatch {
        return nil, exception.NewError(
            notAcceptableMessage,
            exceptioncontract.Context{"accept": acceptHeader},
            ErrNotAcceptable,
        )
    }

    serializerInstance, exists := instance.defaultSerializer()
    if true == exists {
        return serializerInstance, nil
    }

    return nil, exception.NewError("no serializer found for accept header", exceptioncontract.Context{"accept": acceptHeader}, nil)
}
