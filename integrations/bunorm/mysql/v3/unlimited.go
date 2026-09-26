package mysql

import (
    "math"
    "time"
)

/* Unlimited lifts the bound of a PoolConfig or TimeoutConfig field: no deadline for a timeout, no cap on the open or idle connections, no recycling for a lifetime or an idle time. Every negative value reads the same, so a duration read from configuration as -1s lifts it too; a zero field is unset and takes the constructor default, so a configuration assembled from environment keys nobody set keeps the bounds. */
const Unlimited = -1

/* resolvedDuration answers a configured bound as the driver and database/sql read it, where zero means no bound. */
func resolvedDuration(configured time.Duration, fallback time.Duration) time.Duration {
    if 0 > configured {
        return 0
    }

    if 0 == configured {
        return fallback
    }

    return configured
}

/* resolvedOpenConnectionCount answers the open-connection cap; zero is the value database/sql reads as no cap. */
func resolvedOpenConnectionCount(configured int, fallback int) int {
    if 0 > configured {
        return 0
    }

    if 0 == configured {
        return fallback
    }

    return configured
}

/* resolvedIdleConnectionCount answers the idle-connection cap; database/sql retains no idle connection for zero and clamps a cap above the open one to it, so no cap is the largest count. */
func resolvedIdleConnectionCount(configured int, fallback int) int {
    if 0 > configured {
        return math.MaxInt
    }

    if 0 == configured {
        return fallback
    }

    return configured
}
