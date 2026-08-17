# ROADMAP

This document lists high-level, forward-looking plans for the Melody repository.

Every item this document used to carry has shipped — static file serving, the firewall system and the router's named routes, url generation, grouping and constraints. What they do now is described where a current capability belongs, in [`./package/`](./package/), not here.

## Direction

- This major line is being driven to complete stability and then frozen; new capabilities land in the newest major line. Remaining work here is hardening: aligning behaviour with what the documentation promises, closing defects, and keeping the example application a faithful showcase.

## Longer-term

- Close remaining gaps
    - Finish levelling this major line with the newest one where a capability it already has is missing or behaves differently, then freeze. New framework capabilities are designed on the newest major line and reach this one only as part of that levelling, never as this line's own additions.
