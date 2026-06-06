# Silo TheIntroDB Plugin

First-party Silo marker provider for [TheIntroDB](https://theintrodb.org).

The plugin implements `marker_provider.v1` and can fetch or submit `intro`,
`credits`, `recap`, and `preview` markers for movies and TV episodes.

## Configuration

The `account` global config accepts an optional `api_key` string. Fetches work
without a key; submissions and account statistics require one.

## Development

```sh
GOWORK=off go test ./...
make build
```
