#!/usr/bin/env node

const crypto = require("node:crypto");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");

const API_BASE = "https://www.virustotal.com/api/v3";
const DEFAULT_POLL_MS = 20_000;
const DEFAULT_TIMEOUT_MS = 10 * 60_000;
const PUBLIC_RATE_LIMIT_WINDOW_MS = 60_000;
const PUBLIC_RATE_LIMIT_MAX_REQUESTS = 4;

function usage(exitCode = 0) {
  const text = `
Usage:
  VT_API_KEY=... node scripts/virustotal-file.js [options] <file> [file...]

Options:
  --upload-missing     Upload files that are not already known to VirusTotal.
  --wait               Wait for uploaded analyses to complete.
  --poll-ms <ms>       Poll interval when --wait is used. Default: ${DEFAULT_POLL_MS}
  --timeout-s <sec>    Timeout when --wait is used. Default: ${DEFAULT_TIMEOUT_MS / 1000}
  --engine <name>      Print a specific engine result. Repeat to request multiple engines.
  --json               Print raw VirusTotal JSON for each resolved file object.
  -h, --help           Show this help.

Notes:
  - By default the script only does a hash lookup and does not upload anything.
  - --upload-missing submits the file to VirusTotal. Uploaded files may be shared by VirusTotal.
  - The public API is rate-limited by default to stay within the documented 4 requests/minute.
`;
  const stream = exitCode === 0 ? process.stdout : process.stderr;
  stream.write(text.trimStart());
  process.exit(exitCode);
}

function fail(message, exitCode = 1) {
  process.stderr.write(`${message}\n`);
  process.exit(exitCode);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function parseArgs(argv) {
  const options = {
    uploadMissing: false,
    wait: false,
    pollMs: DEFAULT_POLL_MS,
    timeoutMs: DEFAULT_TIMEOUT_MS,
    engines: [],
    json: false,
    files: [],
  };

  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--upload-missing") {
      options.uploadMissing = true;
      continue;
    }
    if (arg === "--wait") {
      options.wait = true;
      continue;
    }
    if (arg === "--json") {
      options.json = true;
      continue;
    }
    if (arg === "-h" || arg === "--help") {
      usage(0);
    }
    if (arg === "--poll-ms") {
      const next = argv[i + 1];
      if (!next) {
        fail("--poll-ms requires a value");
      }
      options.pollMs = Number.parseInt(next, 10);
      i += 1;
      continue;
    }
    if (arg === "--timeout-s") {
      const next = argv[i + 1];
      if (!next) {
        fail("--timeout-s requires a value");
      }
      options.timeoutMs = Number.parseInt(next, 10) * 1000;
      i += 1;
      continue;
    }
    if (arg === "--engine") {
      const next = argv[i + 1];
      if (!next) {
        fail("--engine requires a value");
      }
      options.engines.push(next);
      i += 1;
      continue;
    }
    if (arg.startsWith("-")) {
      fail(`Unknown option: ${arg}`);
    }
    options.files.push(arg);
  }

  if (!Number.isFinite(options.pollMs) || options.pollMs < 15_000) {
    fail("--poll-ms must be at least 15000 to stay under public API rate limits");
  }
  if (!Number.isFinite(options.timeoutMs) || options.timeoutMs <= 0) {
    fail("--timeout-s must be greater than 0");
  }
  if (options.files.length === 0) {
    usage(1);
  }

  return options;
}

async function sha256File(filePath) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("sha256");
    const stream = fs.createReadStream(filePath);
    stream.on("error", reject);
    stream.on("data", (chunk) => hash.update(chunk));
    stream.on("end", () => resolve(hash.digest("hex")));
  });
}

class VirusTotalClient {
  constructor(apiKey) {
    this.apiKey = apiKey;
    this.requestTimes = [];
  }

  async throttle() {
    const now = Date.now();
    this.requestTimes = this.requestTimes.filter(
      (ts) => now - ts < PUBLIC_RATE_LIMIT_WINDOW_MS,
    );
    if (this.requestTimes.length < PUBLIC_RATE_LIMIT_MAX_REQUESTS) {
      return;
    }
    const waitMs =
      PUBLIC_RATE_LIMIT_WINDOW_MS - (now - this.requestTimes[0]) + 250;
    if (waitMs > 0) {
      await sleep(waitMs);
    }
    const nextNow = Date.now();
    this.requestTimes = this.requestTimes.filter(
      (ts) => nextNow - ts < PUBLIC_RATE_LIMIT_WINDOW_MS,
    );
  }

  async request(url, init = {}, expectedStatuses = [200]) {
    await this.throttle();

    const headers = new Headers(init.headers || {});
    headers.set("x-apikey", this.apiKey);

    const response = await fetch(url, { ...init, headers });
    this.requestTimes.push(Date.now());

    if (!expectedStatuses.includes(response.status)) {
      const text = await response.text();
      const message = text.trim() || response.statusText;
      throw new Error(`${response.status} ${response.statusText}: ${message}`);
    }
    return response;
  }

  async getFileByHash(hash) {
    const response = await this.request(
      `${API_BASE}/files/${hash}`,
      {},
      [200, 404],
    );
    if (response.status === 404) {
      return null;
    }
    return response.json();
  }

  async getLargeFileUploadUrl() {
    const response = await this.request(`${API_BASE}/files/upload_url`);
    return response.json();
  }

  async uploadFile(filePath) {
    const stat = await fsp.stat(filePath);
    let uploadUrl = `${API_BASE}/files`;
    if (stat.size > 32 * 1024 * 1024) {
      const payload = await this.getLargeFileUploadUrl();
      uploadUrl = payload.data;
    }

    const blob = await fs.openAsBlob(filePath);
    const form = new FormData();
    form.append("file", blob, path.basename(filePath));

    const response = await this.request(uploadUrl, {
      method: "POST",
      body: form,
    });
    return response.json();
  }

  async getAnalysis(analysisId, selfLink) {
    const url = selfLink || `${API_BASE}/analyses/${analysisId}`;
    const response = await this.request(url);
    return response.json();
  }
}

function printHeader(label) {
  process.stdout.write(`\n${label}\n`);
}

function printKeyValue(key, value) {
  process.stdout.write(`${key}: ${value}\n`);
}

function summarizeStats(stats = {}) {
  const keys = [
    "harmless",
    "malicious",
    "suspicious",
    "undetected",
    "timeout",
    "type-unsupported",
    "failure",
  ];
  return keys
    .filter((key) => typeof stats[key] === "number")
    .map((key) => `${key}=${stats[key]}`)
    .join(", ");
}

function printEngineResults(fileObject, requestedEngines) {
  if (requestedEngines.length === 0) {
    return;
  }
  const results = fileObject?.data?.attributes?.last_analysis_results || {};
  process.stdout.write("engines:\n");
  for (const name of requestedEngines) {
    const result = results[name];
    if (!result) {
      process.stdout.write(`- ${name}: not present in report\n`);
      continue;
    }
    const verdict = result.result || result.category || "unknown";
    process.stdout.write(`- ${name}: ${verdict}\n`);
  }
}

function printFileSummary(filePath, sha256, fileObject, requestedEngines) {
  const attrs = fileObject?.data?.attributes || {};
  printHeader(filePath);
  printKeyValue("sha256", sha256);
  printKeyValue("status", "known by VirusTotal");
  if (attrs.meaningful_name) {
    printKeyValue("name", attrs.meaningful_name);
  }
  if (attrs.last_analysis_date) {
    printKeyValue(
      "last_analysis",
      new Date(attrs.last_analysis_date * 1000).toISOString(),
    );
  }
  printKeyValue("stats", summarizeStats(attrs.last_analysis_stats));
  printKeyValue("report", `https://www.virustotal.com/gui/file/${sha256}`);
  printEngineResults(fileObject, requestedEngines);
}

async function waitForAnalysis(client, uploadResponse, options) {
  const analysisId = uploadResponse?.data?.id;
  const selfLink = uploadResponse?.data?.links?.self || uploadResponse?.links?.self;
  if (!analysisId) {
    throw new Error("Upload response did not include an analysis id");
  }

  const startedAt = Date.now();
  while (true) {
    const analysis = await client.getAnalysis(analysisId, selfLink);
    const status = analysis?.data?.attributes?.status || "unknown";
    printKeyValue("analysis", status);
    if (status === "completed") {
      const sha256 =
        analysis?.meta?.file_info?.sha256 ||
        analysis?.data?.meta?.file_info?.sha256;
      return { analysis, sha256 };
    }
    if (Date.now() - startedAt > options.timeoutMs) {
      throw new Error(
        `Timed out waiting for analysis ${analysisId} after ${options.timeoutMs}ms`,
      );
    }
    await sleep(options.pollMs);
  }
}

async function resolveFileObject(client, filePath, options) {
  const sha256 = await sha256File(filePath);
  let fileObject = await client.getFileByHash(sha256);

  if (fileObject) {
    return { sha256, fileObject, uploaded: false };
  }

  printHeader(filePath);
  printKeyValue("sha256", sha256);
  printKeyValue("status", "not known by VirusTotal");

  if (!options.uploadMissing) {
    printKeyValue("hint", "rerun with --upload-missing to submit this file");
    return { sha256, fileObject: null, uploaded: false };
  }

  printKeyValue("upload", "submitting file to VirusTotal");
  const uploadResponse = await client.uploadFile(filePath);

  if (!options.wait) {
    const analysisId = uploadResponse?.data?.id || "unknown";
    printKeyValue("analysis_id", analysisId);
    printKeyValue("hint", "rerun with --wait to poll until analysis completes");
    return { sha256, fileObject: null, uploaded: true };
  }

  const { sha256: analyzedSha256 } = await waitForAnalysis(
    client,
    uploadResponse,
    options,
  );
  const resolvedSha256 = analyzedSha256 || sha256;
  fileObject = await client.getFileByHash(resolvedSha256);
  return { sha256: resolvedSha256, fileObject, uploaded: true };
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const apiKey = process.env.VT_API_KEY || process.env.VIRUSTOTAL_API_KEY;
  if (!apiKey) {
    fail("Set VT_API_KEY or VIRUSTOTAL_API_KEY before running this script.");
  }

  const client = new VirusTotalClient(apiKey);
  let hadError = false;

  for (const filePath of options.files) {
    try {
      await fsp.access(filePath, fs.constants.R_OK);
      const { sha256, fileObject } = await resolveFileObject(
        client,
        filePath,
        options,
      );
      if (fileObject) {
        printFileSummary(filePath, sha256, fileObject, options.engines);
        if (options.json) {
          process.stdout.write(`${JSON.stringify(fileObject, null, 2)}\n`);
        }
      }
    } catch (error) {
      hadError = true;
      printHeader(filePath);
      printKeyValue("error", error.message);
    }
  }

  if (hadError) {
    process.exitCode = 1;
  }
}

main();
