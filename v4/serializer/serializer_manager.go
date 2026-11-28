package serializer

import (
    "errors"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    "github.com/precision-soft/melody/v4/internal"
    serializercontract "github.com/precision-soft/melody/v4/serializer/contract"
)

/* ErrNotAcceptable reports that the accept header refused every media type the manager can produce, or holds more members than the negotiation reads and is refused whole, before any type is weighed, so no representation can be served; the result handler answers 406 to both. */
var ErrNotAcceptable = errors.New("no acceptable media type for the accept header")

const notAcceptableMessage = "the accept header refuses every available media type"

/* SerializerManagerConfig is what a manager negotiates with beyond its serializers; the zero value is the default. */
type SerializerManagerConfig struct {
    /* MaxAcceptMembers bounds the members of an Accept header the negotiation reads: 64 when left out, at least 1, unlimited refused, since the header is the client's and every member is weighed against every serializer. A header holding more is refused whole with ErrNotAcceptable, a member past the bound possibly carrying a refusal. */
    MaxAcceptMembers bound.Count
}

var serializerManagerMaxAcceptMembersRule = bound.Rule[int]{
    Name:    "SerializerManagerConfig.MaxAcceptMembers",
    Default: internal.MaximumSplitOutsideQuotesMembers,
    Minimum: 1,
}

/* NewSerializerManager builds a manager over the serializers keyed by mime; a configuration field outside its rule is refused naming the field. */
func NewSerializerManager(serializersByMime map[string]serializercontract.Serializer, config SerializerManagerConfig) (*SerializerManager, error) {
    maxAcceptMembers, maxAcceptMembersErr := bound.Resolve(config.MaxAcceptMembers, serializerManagerMaxAcceptMembersRule)
    if nil != maxAcceptMembersErr {
        return nil, maxAcceptMembersErr
    }

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

        /* a typed nil is refused with the untyped one, at construction rather than on the request path */
        if true == internal.IsNilInterface(serializerInstance) {
            return nil, exception.NewError(
                "serializer instance is nil",
                exceptioncontract.Context{
                    "mime": normalizedMimeKey,
                },
                nil,
            )
        }

        /* two spellings normalizing to one key are refused, since map order would pick the survivor */
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
        maxAcceptMembers:  maxAcceptMembers.Value,
    }, nil
}

type SerializerManager struct {
    serializersByMime map[string]serializercontract.Serializer
    maxAcceptMembers  int
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

/* defaultSerializer answers the representation served when the header expresses no usable preference: json when registered, otherwise the first serializer in lexical mime order. */
func (instance *SerializerManager) defaultSerializer() (serializercontract.Serializer, bool) {
    return instance.defaultSerializerExcluding(nil)
}

/* defaultSerializerExcluding is defaultSerializer with the types the header refused with q=0 left out. */
func (instance *SerializerManager) defaultSerializerExcluding(refusedMimes map[string]struct{}) (serializercontract.Serializer, bool) {
    if _, refused := refusedMimes[MimeApplicationJson]; false == refused {
        serializerInstance, exists := instance.serializersByMime[MimeApplicationJson]
        if true == exists {
            return serializerInstance, true
        }
    }

    configuredMimes := make([]string, 0, len(instance.serializersByMime))
    for configuredMime := range instance.serializersByMime {
        if _, refused := refusedMimes[configuredMime]; true == refused {
            continue
        }

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

    /* a header with no member the negotiation can read matches no candidate below, as one naming nothing the manager serves does, and both are answered with the default representation */
    acceptedMimes, cut := parseAcceptHeader(acceptHeader, instance.maxAcceptMembers)
    if true == cut {
        return nil, exception.NewError(
            "the accept header holds more members than the negotiation reads and is refused whole",
            exceptioncontract.Context{"accept": acceptHeader},
            ErrNotAcceptable,
        )
    }

    candidateMimes := make([]string, 0, len(instance.serializersByMime))
    for candidateMime := range instance.serializersByMime {
        candidateMimes = append(candidateMimes, candidateMime)
    }

    sort.Strings(candidateMimes)

    /* each type takes the quality of the most specific range covering it, so an exact range overrides a wildcard; a type refused with q=0 is never served, and not acceptable is answered only when every type is refused */
    selectedMime := ""
    selectedQuality := 0.0
    selectedSpecificity := 0
    refusedMimes := map[string]struct{}{}

    for _, candidateMime := range candidateMimes {
        quality, specificity, matched := acceptQualityFor(acceptedMimes, candidateMime)
        if false == matched {
            continue
        }

        if 0 == quality {
            refusedMimes[candidateMime] = struct{}{}

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

            continue
        }

        /* a full tie resolves json first, as the empty header does; other ties keep the lexically first candidate */
        if quality == selectedQuality && specificity == selectedSpecificity && MimeApplicationJson == candidateMime {
            selectedMime = candidateMime
        }
    }

    if "" != selectedMime {
        return instance.serializersByMime[selectedMime], nil
    }

    if len(refusedMimes) == len(candidateMimes) && 0 < len(refusedMimes) {
        return nil, exception.NewError(
            notAcceptableMessage,
            exceptioncontract.Context{"accept": acceptHeader},
            ErrNotAcceptable,
        )
    }

    serializerInstance, exists := instance.defaultSerializerExcluding(refusedMimes)
    if true == exists {
        return serializerInstance, nil
    }

    return nil, exception.NewError("no serializer found for accept header", exceptioncontract.Context{"accept": acceptHeader}, nil)
}
