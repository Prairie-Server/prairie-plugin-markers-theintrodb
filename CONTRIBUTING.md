# Contributing to TheIntroDB Marker Plugin

The [Prairie contribution guide](https://github.com/Prairie-Server/prairie-server/blob/main/CONTRIBUTING.md)
covers project-wide coordination, focused changes, evidence, AI disclosure, and
pull request expectations. Those requirements apply here; this guide adds the
plugin-specific workflow.

## Before you start

Open an [issue](https://github.com/Prairie-Server/prairie-plugin-markers-theintrodb/issues)
before changing marker semantics, submission behavior, configuration, or the
advertised capability. This repository owns the TheIntroDB adapter; contract
changes belong in
[`prairie-plugin-sdk`](https://github.com/Prairie-Server/prairie-plugin-sdk), while host
marker orchestration belongs in
[`prairie-server`](https://github.com/Prairie-Server/prairie-server).

## Development setup

Use the Go version declared in `go.mod`. A local `go.work` may point at a sibling
SDK checkout while developing both repositories, but committed code and CI must
resolve the SDK version pinned in `go.mod` (a release tag or a pseudo-version
of the SDK's `main` branch) with `GOWORK=off`. Never commit a local
filesystem `replace` directive or an API key.

## Validate your change

```sh
GOWORK=off go test ./...
GOWORK=off go vet ./...
GOWORK=off go build ./...
gofmt -l .
GOWORK=off golangci-lint run ./...
GOWORK=off go test ./... -count=1 -covermode=atomic -coverprofile=coverage.out
./scripts/check-coverage.sh coverage.out
```

`gofmt -l .` should print nothing. If it reports unrelated pre-existing drift,
none of the Go files touched by your change may appear in the output; do not add
to the output, and report what remains. Add focused coverage for marker
conversion, validation, upstream error mapping, and authenticated submissions
when those behaviors change.
CI runs golangci-lint v2.14.0 and enforces a 95% statement coverage floor
(`scripts/check-coverage.sh`); the lint and coverage commands above reproduce
those checks locally.
Locally, `golangci-lint run` checks the whole repository, while CI reports only
issues new in the pull request (`only-new-issues`), so the local run is the
stricter of the two.

## Open the pull request

Use a Conventional Commit title, explain any compatibility or marker-accuracy
risk, and paste the actual validation results. Read the
[AI-assisted contribution policy](https://github.com/Prairie-Server/prairie-server/blob/main/docs/ai-contributions.md)
and include its disclosure block.
