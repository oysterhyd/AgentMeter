const fs = require('node:fs');
const path = require('node:path');
const { execFileSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const artifacts = path.join(root, 'artifacts', 'performance-2.8.2');
const version = require('../package.json').version;
const median = values => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)];
const change = (before, after) => (after / before - 1) * 100;
function benchmarks(names) {
  const all = new Map();
  for (const name of names) {
    const current = new Map();
    for (const line of fs.readFileSync(path.join(artifacts, name), 'utf8').split(/\r?\n/)) {
      if (!line.startsWith('Benchmark')) continue;
      const fields = line.trim().split(/\s+/), key = fields[0].replace(/-\d+$/, '');
      const sample = { ns: +fields[fields.indexOf('ns/op') - 1], bytes: +fields[fields.indexOf('B/op') - 1], allocations: +fields[fields.indexOf('allocs/op') - 1] };
      if (!Object.values(sample).every(Number.isFinite)) throw new Error(`Invalid benchmark: ${line}`);
      if (!current.has(key)) current.set(key, []);
      current.get(key).push(sample);
    }
    for (const [key, samples] of current) all.set(key, samples);
  }
  return all;
}
const baseline = benchmarks(['baseline-monitor.txt', 'baseline-recovery.txt', 'baseline-native.txt', 'baseline-client.txt']);
const candidate = benchmarks(['candidate-monitor.txt', 'candidate-native.txt', 'candidate-snapshots-final.txt', 'candidate-client-final.txt', 'candidate-review-final.txt']);
const measurements = {};
for (const [key, before] of baseline) {
  const after = candidate.get(key);
  if (!after) continue;
  if (before.length !== 3 || after.length !== 3) throw new Error(`Expected three samples: ${key}`);
  const entry = { baseline: before, candidate: after, medians: {}, changePercent: {} };
  for (const field of ['ns', 'bytes', 'allocations']) {
    entry.medians[field] = { baseline: median(before.map(s => s[field])), candidate: median(after.map(s => s[field])) };
    entry.changePercent[field] = entry.medians[field].baseline ? change(entry.medians[field].baseline, entry.medians[field].candidate) : 0;
  }
  measurements[key] = entry;
}
function memory(file) {
  const raw = JSON.parse(fs.readFileSync(path.join(artifacts, file), 'utf8'));
  const result = { scenario: raw.scenario, samplingIntervalMs: raw.samplingIntervalMs, runs: raw.measurements.map(({ samples, ...run }) => run), medians: {} };
  for (const field of ['sampledPeakPrivateBytes', 'peakWorkingSetBytes', 'cpuMs']) {
    const before = median(raw.measurements.filter(r => r.build === 'baseline').map(r => r[field]));
    const after = median(raw.measurements.filter(r => r.build === 'candidate').map(r => r[field]));
    result.medians[field] = { baseline: before, candidate: after, changePercent: change(before, after) };
  }
  return result;
}
function size(file) { return fs.statSync(path.join(root, file)).size; }
const sizes = {
  installer: { baseline: size('release/Codex-Monitor-Setup-2.8.1.exe'), candidate: size(`release/Codex-Monitor-Setup-${version}.exe`) },
  executable: { baseline: size('artifacts/performance-2.8.2/baseline-release/Codex Monitor Native.exe'), candidate: size('build/native/Codex Monitor Native.exe') },
  updateArchive: { baseline: size('release/Codex-Monitor-2.8.1-windows-amd64.tar.gz'), candidate: size(`release/Codex-Monitor-${version}-windows-amd64.tar.gz`) },
};
for (const entry of Object.values(sizes)) entry.changePercent = change(entry.baseline, entry.candidate);
const baselineCommit = 'f56496193e0fdc37cb0e38fa232911f75d98caa1';
const goFiles = execFileSync('git', ['ls-tree', '-r', '--name-only', baselineCommit, 'main.go', 'internal/monitor', 'internal/nativeapp'], { cwd: root, encoding: 'utf8' }).trim().split('\n').filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
let beforeLines = 0, afterLines = 0;
for (const file of goFiles) {
  beforeLines += execFileSync('git', ['show', `${baselineCommit}:${file}`], { cwd: root, encoding: 'utf8' }).split('\n').length - 1;
  afterLines += fs.readFileSync(path.join(root, file), 'utf8').split('\n').length - 1;
}
afterLines += fs.readFileSync(path.join(root, 'internal/nativeapp/calls_smoke.go'), 'utf8').split('\n').length - 1;
const report = { version, baselineCommit, baselineHarness: 'Identical benchmark sources and isolated smoke synchronization/call-view helpers; production algorithms from the baseline commit.', go: execFileSync(require('./go.cjs'), ['version'], { encoding: 'utf8' }).trim(), platform: 'Windows amd64', cpu: 'AMD Ryzen 7 8845H', measurements, memory: { fivePages: memory('process-memory.json'), fullHistory: memory('process-memory-full-history.json') }, sizes, applicationGoLines: { baseline: beforeLines, candidate: afterLines, netChange: afterLines - beforeLines } };
fs.writeFileSync(path.join(root, 'docs', 'performance-results.json'), JSON.stringify(report, null, 2) + '\n');
for (const [key, entry] of Object.entries(measurements)) {
  const ns = entry.medians.ns, bytes = entry.medians.bytes;
  console.log(`${key}: ${(ns.baseline / 1e6).toFixed(3)} -> ${(ns.candidate / 1e6).toFixed(3)} ms (${entry.changePercent.ns.toFixed(1)}%); ${(bytes.baseline / 1048576).toFixed(3)} -> ${(bytes.candidate / 1048576).toFixed(3)} MiB (${entry.changePercent.bytes.toFixed(1)}%)`);
}
console.log(JSON.stringify({ memory: report.memory, sizes, applicationGoLines: report.applicationGoLines }, null, 2));
