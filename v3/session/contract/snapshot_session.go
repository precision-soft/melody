package contract

/* SnapshotSession is the optional door through which a Session answers its values, its modified flag and its cleared flag read under one critical section, so a concurrent Clear cannot land between them. The framework session implements it; a session without it is read through All, IsModified and IsCleared, three reads a concurrent Clear can interleave. */
type SnapshotSession interface {
    Snapshot() (values map[string]any, modified bool, cleared bool)
}
