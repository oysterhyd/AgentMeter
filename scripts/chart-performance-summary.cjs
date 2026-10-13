const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const directory = path.join(root, 'artifacts', 'chart-performance');
const median = values => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)];
const sha = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
const benchmarkSource = path.join('internal', 'nativeapp', 'chart_performance_test.go');
assert.equal(sha(path.join(root, benchmarkSource)), sha(path.join(directory, 'baseline-source', benchmarkSource)), 'Before/after benchmark harnesses differ.');
function benchmarks(name) {
  const rows = new Map();
  for (const line of fs.readFileSync(path.join(directory, `${name}-bench.log`), 'utf8').split(/\r?\n/)) {
    const match = /^(Benchmark\S+)-\d+\s+\d+\s+([\d.]+) ns\/op\s+(\d+) B\/op\s+(\d+) allocs\/op$/.exec(line.trim());
    if (!match) continue;
    const row = rows.get(match[1]) || [];
    row.push({ ns: Number(match[2]), bytes: Number(match[3]), allocations: Number(match[4]) });
    rows.set(match[1], row);
  }
  return rows;
}
const before = benchmarks('baseline'), after = benchmarks('candidate');
const measurements = [...before].map(([name, runs]) => {
  const candidate = after.get(name);
  assert.equal(runs.length, 3); assert.equal(candidate?.length, 3);
  const summary = rows => Object.fromEntries(['ns', 'bytes', 'allocations'].map(key => [key, median(rows.map(row => row[key]))]));
  const baseline = summary(runs), optimized = summary(candidate);
  return { name, baseline, candidate: optimized, reductionPercent: Object.fromEntries(Object.keys(baseline).map(key => [key, Number(((1 - optimized[key] / baseline[key]) * 100).toFixed(2))])), raw: { baseline: runs, candidate } };
});
assert.equal(measurements.length, 4);
const native = JSON.parse(fs.readFileSync(path.join(directory, 'native-frames.json'), 'utf8').replace(/^\uFEFF/, ''));
const stats = values => {
  const sorted = [...values].sort((a, b) => a - b);
  return { samples: sorted.length, medianMs: median(sorted), p95Ms: sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * .95))], maxMs: sorted.at(-1) };
};
const nativeSummary = ['baseline', 'candidate'].map(variant => {
  const runs = native.runs.filter(run => run.variant === variant);
  assert.equal(runs.length, 3); assert.ok(runs.every(run => run.passed));
  return { variant, peakPrivateBytes: median(runs.map(run => run.peakPrivateBytes)), peakWorkingSetBytes: median(runs.map(run => run.peakWorkingSetBytes)), pages: ['overview', 'quota'].map(page => {
    const frames = runs.flatMap(run => run.frames.filter(frame => frame.stage === page));
    assert.ok(frames.length > 0);
    return { page, backend: [...new Set(frames.map(frame => frame.backend))], total: stats(frames.map(frame => frame.totalMs)), paint: stats(frames.map(frame => frame.paintMs)) };
  }) };
});
const report = { version: '2.8.5', baselineCommit: 'd39e9210ba5cd7e0e5673c52dd258ba46ff1ba29', benchmarkHarnessSHA256: sha(path.join(root, benchmarkSource)), benchmarkScope: '2400 synthetic points, 1100x260 DIPs, 150% DPI. Three 1s runs; identical warm-up. Full chart-only UI build, path/mask lookup and software composition. Moving changes plot position by 0.1 DIP per frame. Excludes window-wide UI and native GPU presentation; does not measure screen FPS.', measurements, nativeScope: native.scenario + ' Engine elapsed frame time includes presentation waiting and startup/rebuild frames; not pure CPU time or measured screen FPS. Memory peaks cover the entire smoke, including resize, capture and all pages.', native: nativeSummary };
const installer = path.join(root, 'release', 'Codex-Monitor-Setup-2.8.5.exe');
if (fs.existsSync(installer)) {
  report.package = { installerBytes: fs.statSync(installer).size, baselineInstallerBytes: fs.statSync(path.join(root, 'release', 'Codex-Monitor-Setup-2.8.4.exe')).size, nativeExeBytes: fs.statSync(path.join(root, 'build', 'native', 'Codex Monitor Native.exe')).size, archiveBytes: fs.statSync(path.join(root, 'release', 'Codex-Monitor-2.8.5-windows-amd64.tar.gz')).size };
}
fs.writeFileSync(path.join(root, 'docs', 'chart-performance-results.json'), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify({ measurements: measurements.map(({ raw, ...rest }) => rest), native: nativeSummary }, null, 2));
