# X Following Pruner

A personal, policy-safe X following manager. It will show accounts you follow and require an explicit confirmation for every unfollow.

## Run

```bash
npm install
npm run dev
```

Open http://localhost:3000.

## Current status

The runnable starter and product architecture exist. X OAuth, following import, and confirmed unfollow actions are intentionally not connected yet; they require an X developer app and its OAuth credentials.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the API flow, cost model, and rate-limit constraints.
