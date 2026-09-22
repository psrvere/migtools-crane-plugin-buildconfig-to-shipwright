# ADR-0014: This module takes Go 1.26 before mta-crane does, and the mta-crane owner decides whether that stands

Status: proposed. Raised 2026-09-22 (BUILD-2334). Verified against migtools/mta-crane at
`main` (f4f8b559) and shipwright-io/build v0.21.0.
Enhancement proposal: not covered there.

## Context

BUILD-2334 moves `shipwright-io/build` from v0.19.0 to v0.21.0, the first release that
carries the `omitempty` tags on `SingleValue` (BUILD-1743). v0.21.0's own `go.mod` declares
`go 1.26.4`, so this module's `go` directive moves from 1.25.6 to 1.26.4 with it, and
`k8s.io/api` and `k8s.io/apimachinery` move from v0.34.4 to v0.36.4.

Before this change `AGENTS.md` said the module stays on Shipwright v0.19.0 and k8s v0.34 "to
remain buildable with the Go 1.25 toolchain". It never said whose toolchain, and the answer
is mta-crane's: mta-crane compiles this plugin in rather than shipping its binary. That
constraint has never been written down as a rule, so giving it up has nothing to override.

The facts on mta-crane's side, each read from its repository at `main`:

- `go.mod` line 3 is `go 1.25.6`, and the file carries no `toolchain` line.
- It requires this plugin at the released tag `v0.11.0`, so nothing there moves until
  someone bumps that pin.
- Four workflows ask for a literal Go version: `go.yml`, `lint.yml`, `release.yml` and
  `reusable-build-release-binaries.yml` each set `go-version` to 1.25.
  `nightly-release-0.10-build-binaries.yml` inherits it by calling the reusable workflow,
  and `e2e-pr-tester.yaml` uses `go-version-file: go.mod`.
- `GOTOOLCHAIN` is set nowhere in that repository: not in any workflow, not in the
  Makefile. The default is therefore `auto`, and the build steps set
  `GOPROXY=https://proxy.golang.org`, so a 1.25 toolchain downloads 1.26.4 and re-execs
  instead of failing. A build that is offline, or that sets `GOTOOLCHAIN=local`, fails
  outright.
- The toolchain is not the only thing that moves. mta-crane pins `k8s.io/api`,
  `k8s.io/apimachinery` and `k8s.io/client-go` at v0.35.3 and
  `sigs.k8s.io/controller-runtime` at v0.23.3 directly, and holds
  `shipwright-io/build v0.19.0` as an indirect. A plugin release built on v0.21.0 pulls k8s
  to v0.36.x and controller-runtime to v0.24.x there as well, a minor on each, and leaves
  `client-go` a minor behind its two siblings until someone tidies.

One fact on this side changes what the parity promise was still worth: `tests/go.mod` here
already declared `go 1.26.0` before this change, and `.github/workflows/go.yml` sets
`GOTOOLCHAIN: auto` on the step that runs that module's suite. CI has been fetching a 1.26
toolchain for one of the two modules in this repository for some time.

## Decision

Pending the mta-crane owner. This record states the facts and the two options. It does not
settle them, and the pull request that carries the bump says the same.

Option A: mta-crane moves to Go 1.26, k8s v0.36 and controller-runtime v0.24 first, and then
bumps its plugin pin. Parity holds and this module is never ahead of its consumer.

Option B: mta-crane holds its pin at the last plugin release built on Go 1.25, v0.11.0,
until it is ready to move. The two repositories are out of step on purpose for as long as
that pin holds.

What is decided here is only that the choice gets recorded rather than left in a pull
request comment, because the next person to bump Shipwright needs to find it.

## Rules

- The `go` directive follows the dependency that forces it. It is not held back to match a
  consumer, and it is not moved on its own either.
- A change to the Shipwright pin says in the pull request what it does to mta-crane's `go`
  directive and to its k8s and controller-runtime pins, because minimal version selection
  moves those too, not just the toolchain.
- `AGENTS.md` points here instead of stating a parity guarantee. When the answer arrives,
  set the status to accepted and say which option was taken: for Option A name the mta-crane
  pull request, for Option B name the plugin release mta-crane holds.

## Consequences

- With `GOTOOLCHAIN` unset and the proxy reachable, mta-crane's CI keeps building after it
  bumps the pin, because it downloads 1.26.4. The case to check before taking Option B is a
  consumer that builds offline or sets `GOTOOLCHAIN=local`.
- This repository's own CI needs no change: every workflow resolves Go through
  `go-version-file: go.mod`.
- Nothing pins this record. It describes a constraint in another repository, which no test
  here can read, so it goes stale silently and has to be re-read when the Shipwright pin
  next moves.
