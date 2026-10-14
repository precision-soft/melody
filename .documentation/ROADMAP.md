# ROADMAP

This document lists high-level, forward-looking plans for the Melody repository.

Every item this document used to carry has shipped — static file serving, the firewall system and the router's named routes, url generation, grouping and constraints. What they do now is described where a current capability belongs, in [`./package/`](./package/), not here.

## Direction

- This major line is feature-frozen since 2026-08-18 and receives patch-level defect fixes and security work only, through the end-of-support date the [README's version table](../README.md#versions--project-status) gives it; new capabilities land in the newest major line. The work left here is hardening: aligning behaviour with what the documentation promises, closing defects, and keeping the example application a faithful showcase.

## Longer-term

- Close remaining gaps
    - A defect the newest line's hardening uncovers is measured on this line too and repaired here at patch level when this line's code carries it. A capability is not levelled into this line: new framework capabilities are designed on the newest major line and stay there.
