# X Following Pruner — architecture sketch

## Goal

A personal web app: sign in with X, review accounts you follow, and explicitly unfollow selected accounts. It never auto-unfollows.

## Shape

```text
Browser
  ├─ OAuth 2.0 PKCE → X
  ├─ GET /2/users/me
  ├─ GET /2/users/{me}/following (paged, max 1,000/page)
  └─ click “Unfollow” → app → DELETE /2/users/{me}/following/{target}

App server
  ├─ encrypt and store OAuth refresh token
  ├─ cache following records + cursor/checkpoint
  ├─ enqueue a user-confirmed unfollow
  └─ rate-limited worker executes the DELETE and records its result

Postgres
  users, oauth_tokens, following_cache, unfollow_jobs
```

## Minimum components

| Component | Responsibility |
|---|---|
| One Next.js app | OAuth callback, UI, API routes, and worker trigger. |
| Postgres | Tokens, cached following rows, and durable unfollow queue. |
| Cron/worker | Resume scans and process confirmed actions under X limits. |

No separate queue, cache, analytics pipeline, or ML ranking in v1.

## Data

```text
following_cache
  owner_id, target_id, username, name, description, created_at,
  public_metrics, fetched_at

unfollow_jobs
  id, owner_id, target_id, status: queued|running|done|failed,
  requested_at, completed_at, error
```

Keep only fields needed for browsing/filtering. Delete a cached row after a successful unfollow and expire the rest after 30 days.

## Flows

### Connect and scan

1. OAuth 2.0 Authorization Code + PKCE requests `users.read follows.read follows.write offline.access`.
2. Fetch the authenticated user ID.
3. Page through `GET /2/users/{id}/following`; checkpoint `next_token` after every page.
4. Upsert returned accounts; UI can browse completed pages before the scan finishes.
5. Resume from the checkpoint on retry; refresh only on user request.

### Review and unfollow

1. User filters/sorts cached accounts and clicks **Unfollow** on one row.
2. Show a small confirmation with account handle; a bulk-select action remains out of scope.
3. Insert a job only after confirmation.
4. Worker calls `DELETE /2/users/{source}/following/{target}` with that user’s token.
5. On success, mark done and remove the cached account; on `429`, wait for `x-rate-limit-reset`; otherwise surface the failure.

## Cost and scale

| Work | Formula | 1k | 10k | 100k |
|---|---:|---:|---:|---:|
| Initial owned read | `$0.001 × following count` | $1 | $10 | $100 |
| Unfollow writes | `$0.010 × removals` | $10 if all | $100 if all | $1,000 if all |
| Read request count | `ceil(count / 1,000)` | 1 | 10 | 100 |

`GET /following` permits 300 requests per 15 minutes, so reading is fast at this scale. `DELETE /following` permits 50 actions per 15 minutes: 1,000 confirmed unfollows take at least about 5 hours. Queue and throttle actions; do not parallelize around that limit.

For normal use, scan once, cache it, then refresh only when the user asks. Daily full rescans cost `0.001 × following count` every day.

## Constraints

- Use the official X API only.
- Keep every unfollow directly user-initiated and reviewable; no scheduled or bulk auto-unfollow.
- Set a hard per-user queue cap and a monthly X API spend cap before enabling production actions.
- Revoke tokens on disconnect and delete X-derived cached data on the applicable policy timeline.

## Build order

1. OAuth login and a read-only paginated following list.
2. Cache/checkpoint scan state and basic local filters.
3. Single-account confirmation + unfollow queue with rate-limit handling.
4. Spend limit, token revocation, deletion/retention job, and audit view.

## Open product decisions

- Which local filters matter: inactive accounts, low follower ratio, no bio, manual labels, or simply search?
- Is this single-user only, or does it need multi-user SaaS isolation and billing?
