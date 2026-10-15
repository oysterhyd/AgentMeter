const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const go = require('./go.cjs');
const task = process.argv[2] || 'start';
const out = path.join(root, 'build', 'native');
const exe = path.join(out, 'Codex Monitor Native.exe');
const config = require('../mygo.json');
if (config.version !== require('../package.json').version || !fs.readFileSync(path.join(root, 'internal', 'nativeapp', 'version.go'), 'utf8').includes(`const appVersion = "${config.version}"`)) throw new Error('Release version files disagree.');
function run(file, args, options = {}) {
  const result = spawnSync(file, args, { cwd: root, stdio: 'inherit', windowsHide: true, ...options });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status || 1);
}
if (task === 'test') { run(go, ['test', './...']); process.exit(0); }
if (!['build', 'start'].includes(task)) throw new Error(`Unknown task: ${task}`);
// Only remove known generated output, inside this workspace's build directory.
if (!out.startsWith(path.join(root, 'build') + path.sep)) throw new Error('Invalid build output path');
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
const feed = `https://github.com/${config.updates.github}/releases/latest/download/update-windows-amd64.json`;
const flags = `-s -w -H=windowsgui -X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageVersion=${config.version} -X github.com/egoist/mygo.packageUpdateFeed=${feed} -X github.com/egoist/mygo.packageUpdateKey=${config.updates.publicKey}`;
run(go, ['build', '-trimpath', '-tags', 'mygo_noinspector', `-ldflags=${flags}`, '-o', exe, '.']);
fs.copyFileSync(path.join(root, 'assets', 'icon.ico'), path.join(out, 'icon.ico'));
console.log(`Native application: ${exe}`);
if (task === 'start') run(exe, process.argv.slice(3));
