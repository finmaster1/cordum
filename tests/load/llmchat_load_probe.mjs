#!/usr/bin/env node
/**
 * Host-driven cordum-llm-chat load probe for Ollama-default production readiness.
 *
 * This intentionally avoids adding another Compose service. Run from the repo root:
 *
 *   CORDUM_API_KEY=<key> TOTAL=50 TIMEOUT_MS=20000 node tests/load/llmchat_load_probe.mjs
 *
 * Optional env:
 *   LLMCHAT_URL     defaults to http://127.0.0.1:8090/api/v1/chat
 *   TOTAL           number of concurrent POST sessions (default 50)
 *   TIMEOUT_MS      per-request timeout (default 20000)
 *   PRINCIPAL       trusted-forwarder principal for direct llm-chat probes
 */

const url = process.env.LLMCHAT_URL || "http://127.0.0.1:8090/api/v1/chat";
const key = process.env.CORDUM_API_KEY;
const total = Number(process.env.TOTAL || 50);
const timeoutMs = Number(process.env.TIMEOUT_MS || 20_000);
const principal = process.env.PRINCIPAL || "prod-readiness-load";

if (!key) {
  console.error("CORDUM_API_KEY is required");
  process.exit(2);
}
if (!Number.isFinite(total) || total <= 0) {
  console.error(`TOTAL must be positive, got ${process.env.TOTAL}`);
  process.exit(2);
}
if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) {
  console.error(`TIMEOUT_MS must be positive, got ${process.env.TIMEOUT_MS}`);
  process.exit(2);
}

const started = Date.now();

async function one(i) {
  const t0 = Date.now();
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-API-Key": key,
        "X-Cordum-Tenant": "default",
        "X-Cordum-Principal": principal,
        "X-Cordum-Role": "admin",
      },
      body: JSON.stringify({ message: `Load probe ${i}: reply with OK only.` }),
      signal: AbortSignal.timeout(timeoutMs),
    });
    const text = await res.text();
    return { i, status: res.status, ms: Date.now() - t0, body: text.slice(0, 180) };
  } catch (err) {
    return {
      i,
      error: err?.name || "error",
      message: err?.message || String(err),
      ms: Date.now() - t0,
    };
  }
}

const results = await Promise.all(Array.from({ length: total }, (_, i) => one(i + 1)));
const counts = {};
for (const r of results) {
  const k = r.status ? `http_${r.status}` : `${r.error}`;
  counts[k] = (counts[k] || 0) + 1;
}
const times = results.map((r) => r.ms).sort((a, b) => a - b);
function pct(p) {
  return times[Math.min(times.length - 1, Math.floor((p / 100) * times.length))] || 0;
}

console.log(JSON.stringify({
  url,
  total,
  timeoutMs,
  elapsedMs: Date.now() - started,
  counts,
  p50_ms: pct(50),
  p95_ms: pct(95),
  p99_ms: pct(99),
  sample: results.slice(0, 8),
}, null, 2));
