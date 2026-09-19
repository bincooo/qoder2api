# Qoder2API

A Go HTTP server that exposes the [Qoder](https://qoder.sh) coding-agent backend as an
[OpenAI-compatible](https://platform.openai.com/docs/api-reference/chat/create)
`/v1/chat/completions` endpoint — both streaming (SSE) and non-streaming. Point any OpenAI client or
CLI at this bridge instead of the OpenAI API.

Pure-Go port of an earlier Java implementation (now removed from the repo). Stdlib only.

## Run

```bash
export QODER_PAT=<your qoder personal token>
go run ./cmd/qoder2api
```

The server listens on `http://127.0.0.1:8963/v1/chat/completions` by default.

| Env var      | Default     | Purpose                                    |
|--------------|-------------|--------------------------------------------|
| `QODER_PAT`  | *(required)*| Qoder account token used to obtain a job token |
| `QODER_HOST` | `127.0.0.1` | Bind address                               |
| `QODER_PORT` | `8963`      | Listen port                                |

## Build & test

```bash
go build ./...
go test ./...
```

`baseprompt.json` (the Qoder request template) is embedded at build time — no runtime files
dependency.

## How it works

1. On startup, exchanges your personal token for a Qoder job token (`center.qoder.sh`).
2. For each request: maps OpenAI chat messages to Qoder's `agent_chat_generation` schema (from the
   embedded template), signs and streams the call to `api3.qoder.sh`, and re-emits the SSE deltas as
   OpenAI chunks — or aggregates them into one JSON completion for non-streaming calls. Tool calls
   are translated in both directions.
3. The COSY auth layer (custom base64, RSA/AES session, request signing) lives in `internal/cosy`.

See `CLAUDE.md` for the full architecture and the byte-critical protocol invariants.