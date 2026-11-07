#!/usr/bin/env node
import { parseArgs } from "node:util";

import { ask } from "ask4me-sdk";

function printHelp() {
  process.stdout.write(
    [
      "ask4me-cli",
      "",
      "Usage:",
      "  ask4me-cli -h <host> -k <apiKey> [--title <t>] [--body <b>] [--mcd <m>] [--expires <sec>] [--json]",
      "  ask4me-cli -h <host> -k <apiKey> --notify-sendkey <sendkey> [--title <t>] [--body <b>]",
      "",
      "Options:",
      "  -h, --host <url>     Server base URL, e.g. http://localhost:8080",
      "  -k, --key <apiKey>   API key (Bearer token)",
      "  --title <string>     Title",
      "  --body <string>      Body",
      "  --mcd <string>       MCD",
      "  --expires <seconds>  expires_in_seconds",
      "  --notify-sendkey <s> ServerChan SendKey for this request (multi-user mode)",
      "  --apprise-url <url>  Apprise URL for this request; repeatable",
      "  --e2ee-password <p>  Encrypt this request end-to-end (or set ASK4ME_E2EE_PASSWORD)",
      "  --json               Print final result as JSON",
      "  --help               Show help",
      ""
    ].join("\n")
  );
}

function trimSlash(s) {
  return String(s || "").replace(/\/+$/, "");
}

function toIntOrNull(v) {
  const n = Number(v);
  if (!Number.isFinite(n) || n <= 0) return null;
  return Math.floor(n);
}

function normalizeArgv(argv) {
  const longNames = new Set([
    "title",
    "body",
    "mcd",
    "expires",
    "json",
    "help",
    "host",
    "key",
    "notify-sendkey",
    "apprise-url",
    "e2ee-password"
  ]);
  return argv.map((a) => {
    if (a.startsWith("--")) return a;
    if (!a.startsWith("-") || a === "-") return a;
    if (a === "-h" || a === "-k") return a;
    const name = a.slice(1);
    if (longNames.has(name)) return `--${name}`;
    return a;
  });
}

async function main() {
  const argv = normalizeArgv(process.argv.slice(2));
  const { values } = parseArgs({
    args: argv,
    options: {
      host: { type: "string", short: "h" },
      key: { type: "string", short: "k" },
      title: { type: "string" },
      body: { type: "string" },
      mcd: { type: "string" },
      expires: { type: "string" },
      "notify-sendkey": { type: "string" },
      "apprise-url": { type: "string", multiple: true },
      "e2ee-password": { type: "string" },
      json: { type: "boolean" },
      help: { type: "boolean" }
    },
    allowPositionals: true
  });

  if (values.help) {
    printHelp();
    return;
  }

  const host = values.host;
  const apiKey = values.key;
  if (!host || !apiKey) {
    printHelp();
    process.exit(1);
  }

  const endpoint = `${trimSlash(host)}/v1/ask`;
  const expiresInSeconds = toIntOrNull(values.expires);
  const notifySendKey = values["notify-sendkey"];
  const appriseURLs = values["apprise-url"] ?? [];
  const e2eePassword = values["e2ee-password"] || process.env.ASK4ME_E2EE_PASSWORD;
  const notify = {
    ...(notifySendKey ? { serverchan_sendkey: notifySendKey } : {}),
    ...(appriseURLs.length > 0 ? { apprise_urls: appriseURLs } : {})
  };
  const payload = {
    ...(values.title ? { title: values.title } : {}),
    ...(values.body ? { body: values.body } : {}),
    ...(values.mcd ? { mcd: values.mcd } : {}),
    ...(expiresInSeconds ? { expires_in_seconds: expiresInSeconds } : {}),
    ...(Object.keys(notify).length > 0 ? { notify } : {})
  };

  const res = await ask({
    endpoint,
    apiKey,
    payload,
    e2ee: e2eePassword ? { password: e2eePassword } : undefined,
    onEvent: (ev) => {
      process.stdout.write(`${JSON.stringify(ev)}\n`);
    }
  });

  if (values.json) {
    process.stdout.write(
      JSON.stringify(
        {
          request_id: res.requestId,
          result: res.result ?? null
        },
        null,
        2
      ) + "\n"
    );
  } else if (res.result) {
    const d = res.result.data || {};
    const action = typeof d.action === "string" ? d.action : "";
    const text = typeof d.text === "string" ? d.text : "";
    process.stdout.write(
      `final: ${res.result.type}${action ? ` action=${action}` : ""}${text ? ` text=${JSON.stringify(text)}` : ""}\n`
    );
  } else {
    process.stdout.write("final: unknown\n");
  }

  if (res.result?.type === "user.submitted") process.exit(0);
  if (res.result?.type === "request.expired") process.exit(2);
  process.exit(1);
}

await main();
