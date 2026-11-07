import { createCipheriv, createDecipheriv, pbkdf2Sync, randomBytes } from "node:crypto";

const PREFIX = "enc:v1:";
const AAD = Buffer.from("ask4me-e2ee-v1", "utf8");
const DEFAULT_ITERATIONS = 310000;
const SALT_BYTES = 16;
const NONCE_BYTES = 12;
const TAG_BYTES = 16;

function toBase64Url(buf) {
  return buf.toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function fromBase64Url(s) {
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4));
  return Buffer.from(s.replace(/-/g, "+").replace(/_/g, "/") + pad, "base64");
}

function normalizePassword(password) {
  return String(password ?? "").normalize("NFC");
}

export function deriveKey(password, saltBase64Url, iterations = DEFAULT_ITERATIONS) {
  const salt = fromBase64Url(String(saltBase64Url ?? ""));
  if (salt.length !== SALT_BYTES) {
    throw new Error("e2ee salt must be 16 bytes encoded as base64url");
  }
  const iter = Number(iterations) || DEFAULT_ITERATIONS;
  return pbkdf2Sync(Buffer.from(normalizePassword(password), "utf8"), salt, iter, 32, "sha256");
}

function encryptWithNonce(plain, key, nonce) {
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  cipher.setAAD(AAD);
  const ct = Buffer.concat([cipher.update(Buffer.from(String(plain ?? ""), "utf8")), cipher.final()]);
  const tag = cipher.getAuthTag();
  return PREFIX + toBase64Url(Buffer.concat([nonce, ct, tag]));
}

export function isToken(value) {
  return typeof value === "string" && value.startsWith(PREFIX);
}

export function encryptString(plain, key) {
  return encryptWithNonce(plain, key, randomBytes(NONCE_BYTES));
}

export function decryptText(token, key) {
  if (!isToken(token)) return token;
  const raw = fromBase64Url(token.slice(PREFIX.length));
  if (raw.length < NONCE_BYTES + TAG_BYTES) throw new Error("invalid e2ee token");
  const nonce = raw.subarray(0, NONCE_BYTES);
  const tag = raw.subarray(raw.length - TAG_BYTES);
  const ct = raw.subarray(NONCE_BYTES, raw.length - TAG_BYTES);
  const decipher = createDecipheriv("aes-256-gcm", key, nonce);
  decipher.setAAD(AAD);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(ct), decipher.final()]).toString("utf8");
}

export function encryptJSON(value, key) {
  return encryptString(JSON.stringify(value ?? null), key);
}

export function decryptJSONValue(value, key) {
  if (!isToken(value)) return value;
  const plain = decryptText(value, key);
  try {
    return JSON.parse(plain);
  } catch {
    return plain;
  }
}

export function decryptStringsDeep(value, key) {
  if (typeof value === "string") {
    return isToken(value) ? decryptText(value, key) : value;
  }
  if (Array.isArray(value)) return value.map((v) => decryptStringsDeep(v, key));
  if (value && typeof value === "object") {
    const out = {};
    for (const [k, v] of Object.entries(value)) out[k] = decryptStringsDeep(v, key);
    return out;
  }
  return value;
}

export function replaceTokens(text, key) {
  if (typeof text !== "string" || !text.includes(PREFIX)) return text;
  return text.replace(/enc:v1:[A-Za-z0-9_-]+/g, (token) => decryptText(token, key));
}

function encryptDisplayStrings(value, key, parentKey) {
  if (Array.isArray(value)) return value.map((v) => encryptDisplayStrings(v, key, parentKey));
  if (!value || typeof value !== "object") return value;
  const out = {};
  for (const [k, v] of Object.entries(value)) {
    if (typeof v === "string" && ["title", "description", "label", "text"].includes(k) && v.trim()) {
      out[k] = encryptString(v, key);
    } else {
      out[k] = encryptDisplayStrings(v, key, k);
    }
  }
  return out;
}

export function encryptJSONForms(jsonforms, key) {
  const out = { ...jsonforms };
  if (out.data !== undefined && out.data !== null) {
    out.data = encryptJSON(out.data, key);
  }
  if (out.schema) out.schema = encryptDisplayStrings(out.schema, key);
  if (out.uischema) out.uischema = encryptDisplayStrings(out.uischema, key);
  if (typeof out.submit_label === "string" && out.submit_label.trim()) {
    out.submit_label = encryptString(out.submit_label, key);
  }
  return out;
}

function encryptMCD(mcd, key) {
  let out = mcd.replace(/^(\s*-\s*\[)(.*?)(\]\(.*)$/gm, (line, head, label, tail) => {
    if (!label.trim()) return line;
    return head + encryptString(label, key) + tail;
  });
  out = out.replace(/\blabel="([^"]*)"/g, (m, value) => {
    if (!value.trim()) return m;
    return 'label="' + encryptString(value, key) + '"';
  });
  out = out.replace(/\bsubmit="([^"]*)"/g, (m, value) => {
    if (!value.trim()) return m;
    return 'submit="' + encryptString(value, key) + '"';
  });
  return out;
}

export function preparePayload(payload, password, options = {}) {
  const source = payload && typeof payload === "object" ? payload : {};
  const salt = randomBytes(SALT_BYTES);
  const iterations = Number(options.iterations) || DEFAULT_ITERATIONS;
  const saltB64 = toBase64Url(salt);
  const key = deriveKey(password, saltB64, iterations);
  const out = { ...source };

  if (options.encryptTitle !== false && typeof out.title === "string" && out.title.trim()) {
    out.title = encryptString(out.title, key);
  }
  if (typeof out.body === "string" && out.body.trim()) {
    out.body = encryptString(out.body, key);
  }
  if (typeof out.mcd === "string" && out.mcd.trim()) {
    out.mcd = encryptMCD(out.mcd, key);
  }
  if (out.jsonforms && typeof out.jsonforms === "object") {
    out.jsonforms = encryptJSONForms(out.jsonforms, key);
  }
  out.e2ee = {
    enabled: true,
    salt: saltB64,
    iterations,
    answers: options.encryptAnswers !== false
  };
  return { payload: out, key, salt: saltB64, iterations };
}

export function decryptResult(data, key) {
  if (!data || typeof data !== "object") return data;
  const out = { ...data };
  if (typeof out.text === "string" && isToken(out.text)) {
    out.text = decryptText(out.text, key);
  }
  if (typeof out.payload === "string" && isToken(out.payload)) {
    out.payload = decryptJSONValue(out.payload, key);
  } else if (out.payload && typeof out.payload === "object") {
    out.payload = decryptStringsDeep(out.payload, key);
  }
  return out;
}

export { DEFAULT_ITERATIONS };
