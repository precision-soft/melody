package config

import (
    "context"
    "os"
    "strings"
    "testing"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v4"
    melodyrueidiscache "github.com/precision-soft/melody/integrations/rueidis/v4/cache"
    examplecache "github.com/precision-soft/melody/v4/.example/cache"
    melodyconfig "github.com/precision-soft/melody/v4/config"
    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
    melodyhttpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/redis/rueidis"
)

func TestCacheKeyPrefix_CarriesTheLayoutTokenInsideTheNamespace(t *testing.T) {
    prefix := cacheKeyPrefix("melody-example-v4")

    if false == strings.HasPrefix(prefix, "melody-example-v4:cache:") {
        t.Fatalf("expected the prefix to stay inside the namespace, got %q", prefix)
    }

    if "melody-example-v4:cache:"+examplecache.LayoutToken()+":" != prefix {
        t.Fatalf("expected the layout token between the namespace and the key, got %q", prefix)
    }
}

func TestCatalogWriteThrottle_AllowsThirtyWritesAMinute(t *testing.T) {
    if 30 != catalogWriteAllowance || time.Minute != catalogWriteWindow {
        t.Fatalf("expected thirty writes a minute, got %d per %s", catalogWriteAllowance, catalogWriteWindow)
    }
}

func TestRedisInfrastructure_ProvidesTheTokenStoreAndTheCacheModules(t *testing.T) {
    modules := newRedisInfrastructure("melody-example-v4", nil, melodyrueidis.ConnectionParameters{}).Modules()

    if 2 != len(modules) {
        t.Fatalf("expected the token store and the cache modules, got %d modules", len(modules))
    }

    if _, isTokenStore := modules[0].(*melodyrueidis.Module); false == isTokenStore {
        t.Fatalf("expected the rueidis module first, got %T", modules[0])
    }

    if _, isCache := modules[1].(*melodyrueidiscache.Module); false == isCache {
        t.Fatalf("expected the rueidis cache module second, got %T", modules[1])
    }
}

func TestRedis_TwoNamespacesOverOneClientWriteDisjointKeys(t *testing.T) {
    address := os.Getenv("REDIS_ADDRESS")
    if "" == address {
        t.Skip("REDIS_ADDRESS not set; skipping the redis namespace test")
    }

    client, clientErr := rueidis.NewClient(rueidis.ClientOption{InitAddress: []string{address}})
    if nil != clientErr {
        t.Fatalf("could not connect to redis: %v", clientErr)
    }
    defer client.Close()

    key := "address:" + t.Name()

    for _, namespace := range []string{"melody-example-v4-shop", "melody-example-v4-store"} {
        moduleInstance := moduleWithEnvironment(t, map[string]string{melodyconfig.NamespaceKey: namespace})
        if namespace != moduleInstance.namespace() {
            t.Fatalf("expected the module to read the namespace %q, got %q", namespace, moduleInstance.namespace())
        }

        moduleInstance.redisProvider = melodyrueidis.NewProvider(moduleInstance.namespace(), melodyrueidis.ProviderConfig{})

        containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
        containerInstance.MustRegister(
            melodyrueidis.ServiceConnection,
            func(resolver melodycontainercontract.Resolver) (*melodyrueidis.Connection, error) {
                return melodyrueidis.NewConnection(client, namespace), nil
            },
            melodycontainer.RegisterOptions{},
        )
        moduleInstance.registerRedisServices(containerRegistrar{Container: containerInstance})

        runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())
        limiter := melodyruntime.MustFromRuntime[melodyhttpcontract.RateLimiter](runtimeInstance, serviceCatalogWriteLimiter)
        defer func() {
            _ = limiter.Reset(runtimeInstance, key)
        }()

        if decision, allowErr := limiter.Allow(runtimeInstance, key, melodyhttpcontract.AllowOptions{}); nil != allowErr || catalogWriteAllowance-1 != decision.Remaining {
            t.Fatalf("%s: expected a fresh counter of its own, got %+v %v", namespace, decision, allowErr)
        }

        counterKey := namespace + ":rate_limit:" + catalogWriteLimiterName + ":" + key
        if exists, existsErr := client.Do(context.Background(), client.B().Exists().Key(counterKey).Build()).AsInt64(); nil != existsErr || 1 != exists {
            t.Fatalf("%s: expected the counter under %q, got %d %v", namespace, counterKey, exists, existsErr)
        }

        modules := newRedisInfrastructure(namespace, moduleInstance.redisProvider, melodyrueidis.ConnectionParameters{}).Modules()
        if 2 != len(modules) || false == strings.HasPrefix(cacheKeyPrefix(namespace), namespace+":cache:") || namespace+":token" != redisTokenStoreKeyPrefix(namespace) {
            t.Fatalf("%s: expected the cache and token prefixes inside the namespace", namespace)
        }
    }
}
