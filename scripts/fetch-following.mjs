import { mkdir, rename, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const apiUrl = "https://api.x.com/2";
const userFields = "created_at,description,public_metrics,profile_image_url,verified";

function requireEnv(name) {
  const value = process.env[name];
  if (!value) throw new Error(`Missing ${name}. Copy .env.example to .env.local.`);
  return value;
}

async function request(fetchImpl, url, token) {
  const response = await fetchImpl(url, {
    headers: { Authorization: `Bearer ${token}` },
  });

  if (!response.ok) throw new Error(`X API ${response.status}: ${await response.text()}`);
  return response.json();
}

export async function fetchFollowing({ fetchImpl = fetch, token, username, baseUrl = apiUrl }) {
  const cleanUsername = username.replace(/^@/, "");
  const ownerResult = await request(
    fetchImpl,
    `${baseUrl}/users/by/username/${encodeURIComponent(cleanUsername)}`,
    token,
  );
  const owner = ownerResult.data;
  if (!owner) throw new Error(`X user @${cleanUsername} was not found.`);

  const following = [];
  let nextToken;

  do {
    const params = new URLSearchParams({ max_results: "1000", "user.fields": userFields });
    if (nextToken) params.set("pagination_token", nextToken);

    const page = await request(fetchImpl, `${baseUrl}/users/${owner.id}/following?${params}`, token);
    following.push(...(page.data ?? []));
    nextToken = page.meta?.next_token;
  } while (nextToken);

  return { fetched_at: new Date().toISOString(), owner, following };
}

export async function writeSnapshot(snapshot, outputPath) {
  await mkdir(dirname(outputPath), { recursive: true });
  const temporaryPath = `${outputPath}.tmp`;
  await writeFile(temporaryPath, `${JSON.stringify(snapshot, null, 2)}\n`);
  await rename(temporaryPath, outputPath);
}

async function main() {
  const snapshot = await fetchFollowing({
    token: requireEnv("X_BEARER_TOKEN"),
    username: requireEnv("X_USERNAME"),
  });
  const outputPath = resolve(fileURLToPath(new URL("../data/following.json", import.meta.url)));
  await writeSnapshot(snapshot, outputPath);
  console.log(`Saved ${snapshot.following.length} accounts to ${outputPath}`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
