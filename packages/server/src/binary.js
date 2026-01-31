import crypto from "node:crypto";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import { pipeline as pipelineCb } from "node:stream";

const pipeline = promisify(pipelineCb);

function getAssetSuffix() {
  const platform = process.platform;
  const arch = process.arch;

  const archMap = {
    x64: "amd64",
    arm64: "arm64"
  };
  const osMap = {
    linux: "linux",
    darwin: "darwin",
    win32: "windows"
  };

  const o = osMap[platform];
  const a = archMap[arch];
  if (!o || !a) {
    throw new Error(`unsupported platform/arch: ${platform}/${arch}`);
  }
  const ext = platform === "win32" ? ".exe" : "";
  return `${o}-${a}${ext}`;
}

function defaultBinaryFileName() {
  const ext = process.platform === "win32" ? ".exe" : "";
  return `ask4me${ext}`;
}

export function getBinaryPath({ packageRoot }) {
  const filename = defaultBinaryFileName();
  return path.join(packageRoot, "vendor", filename);
}

function getDownloadURL({ version }) {
  const explicit = process.env.ASK4ME_SERVER_BINARY_URL;
  if (explicit) return explicit;

  const base =
    process.env.ASK4ME_SERVER_BINARY_BASEURL ||
    "https://github.com/easychen/ask4me/releases/download";

  const suffix = getAssetSuffix();
  const v = String(version || "").trim();
  if (!v) {
    throw new Error("package version is missing");
  }
  const baseNoSlash = base.replace(/\/+$/, "");
  return `${baseNoSlash}/v${v}/ask4me-${suffix}`;
}

async function fetchStream(url) {
  const res = await fetch(url, { redirect: "follow" });
  if (!res.ok || !res.body) {
    const text = await res.text().catch(() => "");
    throw new Error(`download failed: HTTP ${res.status} ${text}`.trim());
  }
  return res.body;
}

async function sha256File(filePath) {
  const hash = crypto.createHash("sha256");
  const rs = fs.createReadStream(filePath);
  await pipeline(rs, hash);
  return hash.digest("hex");
}

export async function ensureBinary({ packageRoot }) {
  const overridePath = process.env.ASK4ME_SERVER_BINARY_PATH;
  if (overridePath) return;

  const outPath = getBinaryPath({ packageRoot });
  if (fs.existsSync(outPath)) return;

  fs.mkdirSync(path.dirname(outPath), { recursive: true });

  const pkgJson = JSON.parse(fs.readFileSync(path.join(packageRoot, "package.json"), "utf-8"));
  const url = getDownloadURL({ version: pkgJson.version });

  const tmp = path.join(os.tmpdir(), `ask4me-server-${Date.now()}-${Math.random().toString(16).slice(2)}${process.platform === "win32" ? ".exe" : ""}`);
  let bodyStream;
  try {
    bodyStream = await fetchStream(url);
  } catch (err) {
    if (process.platform === "win32") {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes("HTTP 404") && !url.endsWith(".exe.exe")) {
        bodyStream = await fetchStream(`${url}.exe`);
      } else {
        throw err;
      }
    } else {
      throw err;
    }
  }
  await pipeline(bodyStream, fs.createWriteStream(tmp));

  const expected = process.env.ASK4ME_SERVER_BINARY_SHA256;
  if (expected) {
    const got = await sha256File(tmp);
    if (got.toLowerCase() !== expected.trim().toLowerCase()) {
      try {
        fs.unlinkSync(tmp);
      } catch {}
      throw new Error(`sha256 mismatch: expected ${expected} got ${got}`);
    }
  }

  try {
    fs.renameSync(tmp, outPath);
  } catch {
    fs.copyFileSync(tmp, outPath);
    fs.unlinkSync(tmp);
  }

  if (process.platform !== "win32") {
    fs.chmodSync(outPath, 0o755);
  }
}
