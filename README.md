# X Following Pruner

A personal, policy-safe X following manager written in Go. It imports your complete following list into a local JSON snapshot for experimentation.

## Setup

1. Create an X developer app and copy its Bearer Token.
2. Copy the local config:

   ```bash
   cp .env.example .env.local
   ```

3. Set `X_BEARER_TOKEN` and `X_USERNAME` (without the `@`).

## Import your following list

```bash
go run ./cmd/pruner fetch-following
```

This writes `data/following.json`, which stays local and is ignored by Git.

## Run the local UI

```bash
go run ./cmd/pruner serve
```

Open http://localhost:3000.

## Verify

```bash
go test ./...
go build ./cmd/pruner
```

The importer only reads X data. OAuth and unfollow actions remain out of scope for now.
