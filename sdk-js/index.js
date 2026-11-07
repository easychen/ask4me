import { preparePayload, decryptResult, deriveKey } from "./e2ee.js";

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function withQuery(urlString, query) {
  const u = new URL(urlString);
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== null && String(v).length > 0) {
      u.searchParams.set(k, String(v));
    }
  }
  return u.toString();
}

function parseSSELine(line) {
  if (!line.startsWith("data:")) return null;
  return line.slice(5).trimStart();
}

async function* sseDataIterator(response) {
  const body = response.body;
  if (!body) return;

  const decoder = new TextDecoder("utf-8");
  let buffer = "";

  for await (const chunk of body) {
    buffer += decoder.decode(chunk, { stream: true });
    while (true) {
      const idx = buffer.indexOf("\n");
      if (idx === -1) break;
      const line = buffer.slice(0, idx);
      buffer = buffer.slice(idx + 1);
      const data = parseSSELine(line.trimEnd());
      if (data !== null) {
        yield data;
      }
    }
  }

  buffer += decoder.decode();
  for (const line of buffer.split("\n")) {
    const data = parseSSELine(line.trimEnd());
    if (data !== null) {
      yield data;
    }
  }
}

export function createClient({ endpoint, apiKey, e2ee, fetchImpl } = {}) {
  if (!endpoint) {
    throw new Error("endpoint is required");
  }
  if (!apiKey) {
    throw new Error("apiKey is required");
  }
  const fetchFn = fetchImpl ?? globalThis.fetch;
  if (!fetchFn) {
    throw new Error("fetch is not available; use Node.js >= 18 or provide fetchImpl");
  }

  return {
    ask: (payload, options = {}) =>
      ask({
        endpoint,
        apiKey,
        e2ee,
        payload,
        fetchImpl: fetchFn,
        ...options
      })
  };
}

export async function ask({
  endpoint,
  apiKey,
  payload,
  e2ee,
  onEvent,
  signal,
  maxRetries = Infinity,
  initialBackoffMs = 200,
  maxBackoffMs = 2000,
  requestId: initialRequestId,
  lastEventId: initialLastEventId,
  fetchImpl
}) {
  if (!endpoint) throw new Error("endpoint is required");
  if (!apiKey) throw new Error("apiKey is required");
  if (!fetchImpl) fetchImpl = globalThis.fetch;
  if (!fetchImpl) throw new Error("fetch is not available");

  let requestId = initialRequestId ?? null;
  let lastEventId = initialLastEventId ?? null;
  let attempt = 0;
  let backoff = initialBackoffMs;

  let e2eeKey = null;
  let requestPayload = payload;
  if (e2ee) {
    if (!e2ee.password) throw new Error("e2ee.password is required");
    if (!requestId) {
      const prepared = preparePayload(payload ?? {}, e2ee.password, e2ee);
      requestPayload = prepared.payload;
      e2eeKey = prepared.key;
    } else if (e2ee.salt && e2ee.iterations) {
      e2eeKey = deriveKey(e2ee.password, e2ee.salt, e2ee.iterations);
    }
  }

  const events = [];
  let done = false;
  let finalEvent = null;

  while (!done) {
    if (attempt > maxRetries) {
      throw new Error("maxRetries exceeded");
    }

    const url = withQuery(endpoint, {
      stream: "true",
      request_id: requestId ?? undefined,
      last_event_id: lastEventId ?? undefined
    });

    const res = await fetchImpl(url, {
      method: "POST",
      headers: {
        Accept: "text/event-stream",
        "Content-Type": "application/json",
        Authorization: `Bearer ${apiKey}`
      },
      body: requestId ? null : JSON.stringify(requestPayload ?? {}),
      signal
    });

    if (!res.ok) {
      const text = await res.text().catch(() => "");
      throw new Error(`HTTP ${res.status}: ${text}`);
    }

    const hdrId = res.headers.get("x-ask4me-request-id");
    if (hdrId && !requestId) requestId = hdrId;

    let sawDoneMarker = false;
    try {
      for await (const dataLine of sseDataIterator(res)) {
        if (dataLine === "[DONE]") {
          sawDoneMarker = true;
          done = true;
          break;
        }
        let ev;
        try {
          ev = JSON.parse(dataLine);
        } catch {
          continue;
        }
        if (ev && typeof ev === "object") {
          if (ev.request_id && !requestId) requestId = ev.request_id;
          if (ev.id) lastEventId = ev.id;

          if (e2ee && e2ee.password && ev.type === "request.created" && ev.data && ev.data.e2ee) {
            if (!e2eeKey) {
              e2eeKey = deriveKey(e2ee.password, ev.data.e2ee.salt, ev.data.e2ee.iterations);
            }
          }
          if (e2eeKey && ev.type === "user.submitted" && ev.data) {
            ev = { ...ev, data: decryptResult(ev.data, e2eeKey) };
          }

          events.push(ev);
          if (typeof onEvent === "function") onEvent(ev);
          if (ev.type === "user.submitted" || ev.type === "request.expired") {
            finalEvent = ev;
          }
        }
      }
    } finally {
      try {
        res.body?.cancel?.();
      } catch {}
    }

    if (done) break;
    if (finalEvent && !sawDoneMarker) {
      done = true;
      break;
    }

    if (!requestId) {
      throw new Error("stream ended before request_id was received");
    }

    attempt += 1;
    await sleep(backoff);
    backoff = Math.min(maxBackoffMs, Math.floor(backoff * 1.6));
  }

  return {
    requestId,
    lastEventId,
    events,
    result: finalEvent
  };
}
