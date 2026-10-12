const fs = require('node:fs');
const path = require('node:path');
const root = path.resolve(__dirname, '..');
const input = process.argv[2] || path.join(root, 'artifacts/gui-memory/final-memory.json');
const output = process.argv[3] || path.join(root, 'docs/gui-memory-results.json');
const data = JSON.parse(fs.readFileSync(input, 'utf8').replace(/^\uFEFF/, ''));
const runs = data.measurements;
const median = values => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)];
function pair(before, after) {
  if (!before.length || !after.length || [...before, ...after].some(v => !Number.isFinite(v))) throw new Error('Missing or invalid measurements');
  return { before: { samples: before, median: median(before), min: Math.min(...before), max: Math.max(...before) }, after: { samples: after, median: median(after), min: Math.min(...after), max: Math.max(...after) }, changePercent: (median(after) / median(before) - 1) * 100 };
}
function comparison(read) {
  return pair(runs.filter(r => r.build === 'baseline').map(read), runs.filter(r => r.build === 'candidate').map(read));
}
const peaks = {};
for (const key of ['sampledPeakPrivateBytes', 'sampledPeakPrivateWorkingSetBytes', 'peakWorkingSetBytes', 'cpuMs', 'durationMs']) peaks[key] = comparison(r => r[key]);
const stages = {};
for (const stage of Object.keys(runs[0].stages)) {
  if (stage === 'startup') continue;
  stages[stage] = {};
  for (const key of ['medianPrivateBytes', 'medianPrivateWorkingSetBytes', 'medianWorkingSetBytes']) stages[stage][key] = comparison(r => r.stages[stage][key]);
  stages[stage].heapAlloc = comparison(r => r.heap.find(h => h.stage === stage).heapAlloc);
}
const transitions = {};
for (const stage of ['card', 'cold-card']) transitions[stage] = comparison(r => r.transitions.find(t => t.stage === stage).prepareMs);
const summary = {
  baseline: { version: '2.8.2', commit: '04f20703979b0e65837c4a5b5f6adaf4caff357b', changes: 'Only shared --smoke-memory helper and entry, no product optimization or memory policy.' },
  candidate: { version: '2.8.3', gcPercent: 50, minimumGoMemoryBudgetBytes: 80 << 20, gpu: 'unchanged' },
  scenario: data.scenario,
  stageNotes: { 'cold-card': 'Widget reopened after idle release; the model is still collapsed, so this measures the orb. A real cold expanded card is separately verified in native acceptance.' },
  method: 'Windows process counters every 100 ms; three alternating rounds; state sample median over its last 2 seconds, then median across runs. Same executable basename and production link flags. No screenshots or diagnostic forced GC; product idle reclamation remains enabled.',
  environment: { os: 'Windows amd64', cpu: 'AMD Ryzen 7 8845H', go: '1.27.1', mygo: '0.2.15', logicalProcessors: 16 },
  peaks, stages, transitions,
  runs: runs.map(({samples, ...run}) => ({...run, sampleCount: samples.length})),
};
const benchmarkFile = path.join(root, 'artifacts/gui-memory/background-benchmark.log');
if (fs.existsSync(benchmarkFile)) {
  const benchmarks = { false: [], true: [] };
  for (const match of fs.readFileSync(benchmarkFile, 'utf8').matchAll(/BenchmarkGUIBackground\/(false|true)-\d+\s+\d+\s+(\d+) ns\/op\s+(\d+) B\/op\s+(\d+) allocs\/op/g)) {
    benchmarks[match[1]].push({ timeNs: +match[2], bytesPerOp: +match[3], allocationsPerOp: +match[4] });
  }
  summary.backgroundBenchmark = { scenario: 'Fresh ui.Render at 1380x960 DIP, 150% DPI; includes Tester construction and initial scale-1 drawing. Cumulative allocations, not resident memory.', samples: benchmarks };
  for (const key of ['timeNs', 'bytesPerOp', 'allocationsPerOp']) summary.backgroundBenchmark[key] = pair(benchmarks.false.map(b => b[key]), benchmarks.true.map(b => b[key]));
}
summary.distribution = {};
for (const [key, before, after] of [
  ['installer', 'release/Codex-Monitor-Setup-2.8.2.exe', 'release/Codex-Monitor-Setup-2.8.3.exe'],
  ['archive', 'release/Codex-Monitor-2.8.2-windows-amd64.tar.gz', 'release/Codex-Monitor-2.8.3-windows-amd64.tar.gz'],
]) {
  if (fs.existsSync(path.join(root, before)) && fs.existsSync(path.join(root, after))) summary.distribution[key] = pair([fs.statSync(path.join(root, before)).size], [fs.statSync(path.join(root, after)).size]);
}
fs.writeFileSync(output, JSON.stringify(summary, null, 2) + '\n');
const mib = bytes => (bytes / 1048576).toFixed(1);
for (const [name, v] of Object.entries(peaks)) console.log(`${name}: ${name.endsWith('Bytes') ? mib(v.before.median) + ' -> ' + mib(v.after.median) + ' MiB' : v.before.median.toFixed(3) + ' -> ' + v.after.median.toFixed(3)} (${v.changePercent.toFixed(1)}%)`);
for (const [name, metrics] of Object.entries(stages)) {
  console.log(`${name}: ` + Object.entries(metrics).map(([key, v]) => `${key} ${mib(v.before.median)} -> ${mib(v.after.median)} (${v.changePercent.toFixed(1)}%)`).join('; '));
}
for (const [name, v] of Object.entries(transitions)) console.log(`${name} prepare: ${v.before.median.toFixed(3)} -> ${v.after.median.toFixed(3)} ms`);
console.log(`Report: ${output}`);
