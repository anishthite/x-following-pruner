import assert from "node:assert/strict";
import { fetchFollowing } from "./fetch-following.mjs";

const urls = [];
const responses = [
  { data: { id: "owner", username: "anish" } },
  { data: [{ id: "1", username: "one" }], meta: { next_token: "next" } },
  { data: [{ id: "2", username: "two" }], meta: {} },
];

const snapshot = await fetchFollowing({
  token: "token",
  username: "@anish",
  baseUrl: "https://example.test/2",
  fetchImpl: async (url) => {
    urls.push(url);
    return { ok: true, json: async () => responses.shift() };
  },
});

assert.equal(snapshot.owner.id, "owner");
assert.deepEqual(snapshot.following.map(({ id }) => id), ["1", "2"]);
assert.match(urls[2], /pagination_token=next/);
console.log("fetch-following pagination check passed");
