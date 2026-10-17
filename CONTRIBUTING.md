# Contributing to Vuepher

Thanks for helping. Vuepher aims to be an approachable, well-documented
framework — contributions to docs and examples are as valuable as code.

## Prerequisites

- Go ≥ 1.22
- Node.js LTS (development and build only; never needed at runtime)
- `make`

## Getting set up

```bash
git clone https://github.com/vuepher/vuepher
cd vuepher
go mod download
make vue-install
make verify
```

`make verify` runs formatting, `go vet`, the Go test suite and the frontend
type-check. Please make sure it is green before opening a pull request.

## Project conventions

- **Go:** standard library layout, one package per directory, no import cycles.
  Run `gofmt -s`. Exported identifiers carry doc comments.
- **Templates:** embedded stubs live in `stubs/` as `text/template` files. They
  are embedded through `support.StubFS` and can be overridden per project by
  `stubs/` in the application directory.
- **Errors:** user-facing errors must be actionable — say what went wrong *and*
  how to fix it. Internal errors wrap with `%w`.
- **No new dependency lightly.** If the standard library or an existing
  dependency will do, use it. New dependencies need a one-line justification in
  the pull request.
- **Frontend:** TypeScript, Composition API, `<script setup lang="ts">`.

## Adding a generator

1. Add a `text/template` file under `stubs/`.
2. Create the normalisation helper in `internal/generator/` if the target name
   needs pluralising or case-converting.
3. Register the stub in `support/stubs.go` (`StubFS`).
4. Add the CLI command under `internal/cli/`.
5. Add a test asserting the rendered output — golden files live in
   `internal/generator/testdata/`.

## Architecture decisions

Non-trivial decisions get an ADR in `docs/adr/`, named
`NNNN-short-title.md`, following the template in `docs/adr/0000-template.md`.
The open questions from the PRD (router choice, ORM syntax, migration format,
hot-reload strategy) are tracked there.

## Commit style

Short imperative subject line, then a blank line and a body explaining *why* the
change is needed. Reference the PRD requirement ID (`HTTP-07`, `VIEW-04`, …) when
the change implements one.

## Reporting bugs

Include the output of `vuepher doctor`, the Go and Node versions, your operating
system, and a minimal reproduction. Security issues should not be filed as
public issues — see `SECURITY.md`.
