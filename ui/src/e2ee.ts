const PREFIX = "enc:v1:";
const AAD = new TextEncoder().encode("ask4me-e2ee-v1");
const TOKEN_RE = /enc:v1:[A-Za-z0-9_-]+/g;

function b64urlEncode(bytes: Uint8Array): string {
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function b64urlDecode(s: string): Uint8Array {
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

function concat(a: Uint8Array, b: Uint8Array): Uint8Array {
  const out = new Uint8Array(a.length + b.length);
  out.set(a, 0);
  out.set(b, a.length);
  return out;
}

function subtle(): SubtleCrypto {
  const c = globalThis.crypto;
  if (!c || !c.subtle) {
    throw new Error("WebCrypto is not available; E2EE requires HTTPS or localhost");
  }
  return c.subtle;
}

export function normalizePassword(password: string): string {
  return String(password ?? "").normalize("NFC");
}

export async function deriveKey(password: string, saltB64: string, iterations: number): Promise<CryptoKey> {
  const salt = b64urlDecode(saltB64);
  const baseKey = await subtle().importKey(
    "raw",
    new TextEncoder().encode(normalizePassword(password)),
    "PBKDF2",
    false,
    ["deriveKey"]
  );
  return subtle().deriveKey(
    { name: "PBKDF2", salt, iterations, hash: "SHA-256" },
    baseKey,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"]
  );
}

export function isToken(value: unknown): value is string {
  return typeof value === "string" && value.startsWith(PREFIX);
}

export async function encryptString(plain: string, key: CryptoKey): Promise<string> {
  const nonce = globalThis.crypto.getRandomValues(new Uint8Array(12));
  const ct = await subtle().encrypt(
    { name: "AES-GCM", iv: nonce, additionalData: AAD },
    key,
    new TextEncoder().encode(String(plain))
  );
  return PREFIX + b64urlEncode(concat(nonce, new Uint8Array(ct)));
}

export async function decryptText(token: string, key: CryptoKey): Promise<string> {
  const raw = b64urlDecode(token.slice(PREFIX.length));
  if (raw.length < 12 + 16) throw new Error("invalid e2ee token");
  const nonce = raw.slice(0, 12);
  const ct = raw.slice(12);
  const pt = await subtle().decrypt(
    { name: "AES-GCM", iv: nonce, additionalData: AAD },
    key,
    ct
  );
  return new TextDecoder().decode(pt);
}

export async function encryptJSON(value: unknown, key: CryptoKey): Promise<string> {
  return encryptString(JSON.stringify(value ?? null), key);
}

export async function decryptJSONValue(value: unknown, key: CryptoKey): Promise<unknown> {
  if (!isToken(value)) return value;
  const plain = await decryptText(value, key);
  try {
    return JSON.parse(plain);
  } catch {
    return plain;
  }
}

export async function decryptStringsDeep<T = unknown>(value: T, key: CryptoKey): Promise<T> {
  if (typeof value === "string") {
    if (!isToken(value)) return value;
    return (await decryptText(value, key)) as unknown as T;
  }
  if (Array.isArray(value)) {
    const out: unknown[] = [];
    for (const item of value) out.push(await decryptStringsDeep(item, key));
    return out as unknown as T;
  }
  if (value && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      out[k] = await decryptStringsDeep(v, key);
    }
    return out as T;
  }
  return value;
}

export async function replaceTokens(text: string, key: CryptoKey): Promise<string> {
  const matches = String(text ?? "").match(TOKEN_RE);
  if (!matches || matches.length === 0) return text;
  let out = "";
  let last = 0;
  TOKEN_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = TOKEN_RE.exec(text)) !== null) {
    out += text.slice(last, m.index);
    out += await decryptText(m[0], key);
    last = m.index + m[0].length;
  }
  out += text.slice(last);
  return out;
}
