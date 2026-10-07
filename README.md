# X Following Pruner

A local Go app that imports and visualizes the accounts you follow on X.

## One-time X setup

In the X Developer Console, add this callback URL to the existing app used by Poaster:

```text
http://localhost:3000/auth/callback
```

Copy that app's OAuth 2.0 Client ID and Client Secret into a local config:

```bash
cp .env.example .env.local
```

```env
X_CLIENT_ID=...
X_CLIENT_SECRET=...
```

## Run

```bash
go run ./cmd/pruner serve
```

Open http://localhost:3000 and choose **Import from X**. Approve the `follows.read` permission; the app imports your full following list into `data/following.json`, then displays the visualization.

`data/following.json` and `.env.local` stay local and are ignored by Git.

## Verify

```bash
go test ./...
go build ./cmd/pruner
```
