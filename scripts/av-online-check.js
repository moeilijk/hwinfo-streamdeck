#!/usr/bin/env node
// Look up (and optionally submit) files at OPSWAT MetaDefender Cloud and
// Kaspersky OpenTIP, and report per-engine verdicts.
//
// Usage:
//   METADEFENDER_APIKEY=... OPENTIP_APIKEY=... \
//   node scripts/av-online-check.js [options] <file> [file...]
//
// Options:
//   --service <name>   metadefender or opentip; repeat to select both (default: both)
//   --upload           submit files the service does not know yet (shares the file
//                      with the vendor; off by default)
//   --json-out <dir>   write the raw JSON responses to <dir>/<service>-<sha256>.json
//   --timeout-s <sec>  how long to wait for an upload analysis (default 600)
//   -h, --help
//
// A service whose key is not set is skipped and reported as such. Exit codes:
// 0 no detections, 10 at least one detection, 1 request error.
//
// API references: https://www.opswat.com/docs/mdcloud/metadefender-cloud-api-v4
// and https://github.com/KasperskyLab/OpenTIP-scanner (client.py).

const crypto = require("node:crypto");
const fs = require("node:fs");
const fsp = require("node:fs/promises");
const path = require("node:path");

const MD_BASE = "https://api.metadefender.com/v4";
const OT_BASE = "https://opentip.kaspersky.com/api/v1";

function usage(code = 0) {
  const text = fs
    .readFileSync(__filename, "utf8")
    .split("\n")
    .filter((l) => l.startsWith("//"))
    .slice(0, 22)
    .map((l) => l.replace(/^\/\/ ?/, ""))
    .join("\n");
  (code === 0 ? process.stdout : process.stderr).write(`${text}\n`);
  process.exit(code);
}

function parseArgs(argv) {
  const opts = { services: [], upload: false, jsonOut: null, timeoutS: 600, files: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === "-h" || a === "--help") usage(0);
    else if (a === "--upload") opts.upload = true;
    else if (a === "--service") opts.services.push(argv[++i]);
    else if (a === "--json-out") opts.jsonOut = argv[++i];
    else if (a === "--timeout-s") opts.timeoutS = Number(argv[++i]);
    else if (a.startsWith("-")) usage(2);
    else opts.files.push(a);
  }
  if (opts.services.length === 0) opts.services = ["metadefender", "opentip"];
  for (const s of opts.services) {
    if (!["metadefender", "opentip"].includes(s)) usage(2);
  }
  if (opts.files.length === 0) usage(2);
  return opts;
}

async function sha256(file) {
  const h = crypto.createHash("sha256");
  await new Promise((res, rej) => {
    fs.createReadStream(file).on("data", (d) => h.update(d)).on("end", res).on("error", rej);
  });
  return h.digest("hex");
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function request(url, { method = "GET", headers = {}, body } = {}) {
  const res = await fetch(url, { method, headers, body });
  const text = await res.text();
  let json = null;
  try {
    json = text ? JSON.parse(text) : null;
  } catch {
    json = null;
  }
  return { status: res.status, json, text };
}

async function saveJson(opts, service, hash, data) {
  if (!opts.jsonOut) return;
  await fsp.mkdir(opts.jsonOut, { recursive: true });
  await fsp.writeFile(
    path.join(opts.jsonOut, `${service}-${hash}.json`),
    `${JSON.stringify(data, null, 2)}\n`,
  );
}

// ---------------------------------------------------------------- MetaDefender

function mdSummary(report) {
  const sr = report?.scan_results || {};
  const details = sr.scan_details || {};
  // scan_result_i: 0 clean, 1 infected, 2 suspicious, 23 still in progress on
  // that engine (not a verdict); other codes are skip/error states.
  const detections = Object.entries(details)
    .filter(([, r]) => r && (r.scan_result_i === 1 || r.scan_result_i === 2) && r.threat_found)
    .map(([engine, r]) => `${engine}: ${r.threat_found}`);
  return {
    engines: Object.keys(details).length,
    detected: typeof sr.total_detected_avs === "number" ? sr.total_detected_avs : detections.length,
    overall: sr.scan_all_result_a || "unknown",
    progress: sr.progress_percentage,
    detections,
    // A hash lookup of a file MetaDefender never scanned returns a bare
    // reputation record: a single engine, no start_time, no file type. That is
    // not a scan; only an upload produces a full multi-engine result.
    partial: !sr.start_time && Object.keys(details).length < 5,
  };
}

async function metadefender(file, hash, opts, key) {
  const headers = { apikey: key };
  let { status, json } = await request(`${MD_BASE}/hash/${hash}`, { headers });
  if (status === 200 && json?.scan_results) {
    const summary = mdSummary(json);
    // A bare reputation record is not a scan; with --upload, submit the file
    // anyway so every engine looks at it.
    if (!(summary.partial && opts.upload)) {
      await saveJson(opts, "metadefender", hash, json);
      return { known: true, ...summary };
    }
  }
  if (status !== 404 && status !== 200) {
    throw new Error(`MetaDefender hash lookup HTTP ${status}: ${json?.error?.messages?.join("; ") || ""}`);
  }
  if (!opts.upload) return { known: false };

  const body = await fsp.readFile(file);
  ({ status, json } = await request(`${MD_BASE}/file`, {
    method: "POST",
    headers: { ...headers, filename: path.basename(file), "content-type": "application/octet-stream" },
    body,
  }));
  if (status !== 200 || !json?.data_id) {
    throw new Error(`MetaDefender upload HTTP ${status}: ${json?.error?.messages?.join("; ") || ""}`);
  }
  const dataId = json.data_id;
  const deadline = Date.now() + opts.timeoutS * 1000;
  while (Date.now() < deadline) {
    await sleep(10000);
    ({ status, json } = await request(`${MD_BASE}/file/${dataId}`, { headers }));
    if (status === 200 && json?.scan_results?.progress_percentage === 100) {
      await saveJson(opts, "metadefender", hash, json);
      return { known: true, uploaded: true, ...mdSummary(json) };
    }
  }
  throw new Error(`MetaDefender analysis ${dataId} did not finish within ${opts.timeoutS}s`);
}

// -------------------------------------------------------------------- OpenTIP

function otSummary(report) {
  const info = report?.FileGeneralInfo || {};
  const dets = Array.isArray(report?.DetectionsInfo) ? report.DetectionsInfo : [];
  const detections = dets
    .filter((d) => d && d.DetectionName)
    .map((d) => `Kaspersky: ${d.DetectionName}`);
  const zone = report?.Zone || info.Zone || "unknown";
  const status = info.FileStatus || "unknown";
  return {
    engines: 1,
    detected: zone === "Red" || zone === "Yellow" || detections.length > 0 ? 1 : 0,
    overall: `${zone}/${status}`,
    detections,
  };
}

async function opentip(file, hash, opts, key) {
  const headers = { "x-api-key": key };
  let { status, json } = await request(`${OT_BASE}/search/hash?request=${hash}`, { headers });
  if (status === 200 && json) {
    await saveJson(opts, "opentip", hash, json);
    return { known: true, ...otSummary(json) };
  }
  if (status !== 404 && status !== 200) {
    throw new Error(`OpenTIP hash lookup HTTP ${status}: ${(json && JSON.stringify(json)) || ""}`);
  }
  if (!opts.upload) return { known: false };

  const body = await fsp.readFile(file);
  ({ status, json } = await request(
    `${OT_BASE}/scan/file?filename=${encodeURIComponent(path.basename(file))}`,
    { method: "POST", headers: { ...headers, "content-type": "application/octet-stream" }, body },
  ));
  if (status === 200 && json) {
    await saveJson(opts, "opentip", hash, json);
    return { known: true, uploaded: true, ...otSummary(json) };
  }
  if (status !== 202 && status !== 204) {
    throw new Error(`OpenTIP upload HTTP ${status}: ${(json && JSON.stringify(json)) || ""}`);
  }
  const deadline = Date.now() + opts.timeoutS * 1000;
  while (Date.now() < deadline) {
    await sleep(10000);
    ({ status, json } = await request(`${OT_BASE}/getresult/file?request=${hash}`, { headers }));
    if (status === 200 && json) {
      await saveJson(opts, "opentip", hash, json);
      return { known: true, uploaded: true, ...otSummary(json) };
    }
  }
  throw new Error(`OpenTIP analysis of ${hash} did not finish within ${opts.timeoutS}s`);
}

// ----------------------------------------------------------------------- main

const SERVICES = {
  metadefender: { env: "METADEFENDER_APIKEY", run: metadefender },
  opentip: { env: "OPENTIP_APIKEY", run: opentip },
};

async function main() {
  const opts = parseArgs(process.argv.slice(2));
  let detections = 0;
  let errors = 0;

  for (const file of opts.files) {
    await fsp.access(file, fs.constants.R_OK);
    const hash = await sha256(file);
    process.stdout.write(`\n${file}\nsha256: ${hash}\n`);
    for (const name of opts.services) {
      const svc = SERVICES[name];
      const key = process.env[svc.env];
      if (!key) {
        process.stdout.write(`${name}: skipped (${svc.env} not set)\n`);
        continue;
      }
      try {
        const r = await svc.run(file, hash, opts, key);
        if (!r.known) {
          process.stdout.write(`${name}: not known to the service (rerun with --upload to submit)\n`);
          continue;
        }
        process.stdout.write(
          `${name}: ${r.detected}/${r.engines} detected, overall ${r.overall}${r.uploaded ? ", uploaded" : ""}${r.partial ? " (reputation lookup only, not scanned; rerun with --upload for a full scan)" : ""}\n`,
        );
        for (const d of r.detections) process.stdout.write(`  - ${d}\n`);
        if (r.detected > 0) detections++;
      } catch (err) {
        errors++;
        process.stdout.write(`${name}: error: ${err.message}\n`);
      }
    }
  }

  if (detections > 0) process.exitCode = 10;
  else if (errors > 0) process.exitCode = 1;
}

main().catch((err) => {
  process.stderr.write(`${err.message}\n`);
  process.exit(1);
});
