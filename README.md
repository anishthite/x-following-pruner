# X Following Pruner

A personal, policy-safe X following manager. This first step imports your whole following list into a local JSON file so you can inspect and experiment with it.

## Setup

1. Create an X developer app and copy its Bearer Token.
2. Copy the example config and fill it in:

   ```bash
   cp .env.example .env.local
   ```

   Set `X_BEARER_TOKEN` and your `X_USERNAME` (without the `@`).

3. Fetch your following list:

   ```bash
   npm run fetch:following
   ```

This writes `data/following.json`, which stays local and is ignored by Git.

## Run the UI

```bash
npm run dev
```

Open http://localhost:3000.

## Commands

```bash
npm test          # checks following-list pagination without calling X
npm run lint
npm run build
```

The importer only reads data. OAuth and any unfollow action remain deliberately out of scope for now.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for API cost and rate-limit constraints.
