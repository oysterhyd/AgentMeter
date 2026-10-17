const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const { spawn, spawnSync } = require('node:child_process');
const { once } = require('node:events');
const { compile, files, bundle, installer, version } = require('./package.cjs');
const root = path.resolve(__dirname, '..');
const name = `AgentMeter Installer Check ${process.pid}`;
// A unique stand-in for the 2.x display name; the real one is never touched.
const legacyName = `Legacy Installer Check ${process.pid}`;
const exe = `${name}.exe`;
const uninstallId = `local.codex.monitor.installer-check-${process.pid}`;
const registry = `HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\${uninstallId}`;
const artifacts = path.join(root, 'artifacts');
fs.mkdirSync(artifacts, { recursive: true });
const temporary = fs.mkdtempSync(path.join(artifacts, 'installer-check-'));
const destination = path.join(temporary, '安装目录 with spaces');
const candidate = path.join(temporary, 'AgentMeter-Installer-Check.exe');
const uninstaller = path.join(destination, `Uninstall ${name}.exe`);
function quote(value) { return `'${value.replaceAll("'", "''")}'`; }
function run(file, args, options = {}) {
  const result = spawnSync(file, args, { encoding: 'utf8', windowsHide: true, timeout: 180000, ...options });
  if (result.error) throw result.error;
  assert.equal(result.status, 0, `${file} failed: ${result.stderr || result.stdout}`);
  return result.stdout;
}
function powershell(command) { return run('pwsh', ['-NoProfile', '-Command', `[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false); $ErrorActionPreference = 'Stop'; ${command}`]); }
// NSIS requires /D= and _?= to be the final, unquoted argument, including spaces.
function runInstaller(file, args) { return run(file, args, { windowsVerbatimArguments: true, argv0: `"${file}"` }); }
function digest(file) { return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex'); }
function validateFiles() {
  const expected = files(bundle);
  for (const relative of expected) {
    const installedFile = path.join(destination, relative === 'Codex Monitor Native.exe' ? exe : relative);
    assert.equal(digest(installedFile), digest(path.join(bundle, relative)), `Incorrect installed file: ${relative}`);
  }
  assert.ok(fs.existsSync(uninstaller), 'Missing uninstaller');
  return expected.length;
}
const shortcutPaths = JSON.parse(powershell(`[pscustomobject]@{ Desktop = [Environment]::GetFolderPath('Desktop'); StartMenu = [Environment]::GetFolderPath('Programs') } | ConvertTo-Json -Compress`));
let installed = false;
async function main() {
try {
  assert.ok(fs.existsSync(installer), 'Run npm run package before package:check.');
  compile({ name, legacyName, exe, uninstallId, loginId: name, output: candidate });
  fs.mkdirSync(destination, { recursive: true });
  fs.writeFileSync(path.join(destination, 'keep-user-file.txt'), 'must survive upgrade and uninstall');
  // Simulate a legacy install without registering or launching the old application.
  fs.mkdirSync(path.join(destination, 'resources'));
  fs.writeFileSync(path.join(destination, 'Codex Monitor.exe'), 'legacy executable');
  fs.writeFileSync(path.join(destination, 'Uninstall Codex Monitor.exe'), 'legacy uninstaller');
  fs.writeFileSync(path.join(destination, 'resources', 'app.asar'), 'legacy app source');
  fs.writeFileSync(path.join(destination, 'resources', 'keep-user-file.txt'), 'must survive legacy cleanup');
  // A 2.x install under the previous display name: uninstaller and shortcuts.
  fs.writeFileSync(path.join(destination, `Uninstall ${legacyName}.exe`), 'legacy native uninstaller');
  for (const directory of Object.values(shortcutPaths)) fs.writeFileSync(path.join(directory, `${legacyName}.lnk`), 'legacy shortcut');
  fs.mkdirSync(path.join(destination, 'widget', 'runtime'), { recursive: true });
  fs.writeFileSync(path.join(destination, 'widget', 'runtime', 'electron.exe'), 'legacy widget runtime');
  fs.writeFileSync(path.join(destination, 'widget', 'keep-user-file.txt'), 'must survive widget cleanup');
  runInstaller(candidate, ['/S', `/D=${destination}`]);
  installed = true;
  const count = validateFiles();
  assert.ok(!fs.existsSync(path.join(destination, 'Codex Monitor.exe')), 'Legacy main executable retained');
  assert.ok(!fs.existsSync(path.join(destination, 'resources', 'app.asar')), 'Legacy app archive retained');
  assert.ok(!fs.existsSync(path.join(destination, `Uninstall ${legacyName}.exe`)), 'Renamed uninstaller retained');
  for (const directory of Object.values(shortcutPaths)) assert.ok(!fs.existsSync(path.join(directory, `${legacyName}.lnk`)), 'Renamed shortcut retained');
  assert.ok(!fs.existsSync(path.join(destination, 'widget', 'runtime', 'electron.exe')), 'Legacy widget runtime retained');
  assert.ok(fs.existsSync(path.join(destination, 'widget', 'keep-user-file.txt')), 'Widget user file lost');
  const registration = JSON.parse(powershell(`Get-ItemProperty -LiteralPath ${quote(registry)} | Select-Object DisplayVersion,InstallLocation,UninstallString | ConvertTo-Json -Compress`));
  assert.equal(registration.DisplayVersion, version);
  assert.equal(registration.InstallLocation, destination);
  assert.equal(registration.UninstallString, `"${uninstaller}"`);
  for (const directory of Object.values(shortcutPaths)) {
    const shortcutFile = path.join(directory, `${name}.lnk`);
    const shortcut = JSON.parse(powershell(`$shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut(${quote(shortcutFile)}); [pscustomobject]@{ Target = $shortcut.TargetPath; WorkingDirectory = $shortcut.WorkingDirectory } | ConvertTo-Json -Compress`));
    assert.equal(shortcut.Target, path.join(destination, exe));
    assert.equal(shortcut.WorkingDirectory, destination);
  }
  // Run the installed Go service against a synthetic profile, bypassing the GUI singleton.
  const profile = path.join(temporary, 'profile');
  const snapshotFile = path.join(temporary, 'snapshot.json');
  powershell(`$process = Start-Process -FilePath ${quote(path.join(destination, exe))} -ArgumentList @('--offline', '--data', ${quote(`"${profile}"`)}, '--home', ${quote(`"${profile}"`)}, '--snapshot', '{}') -WindowStyle Hidden -Wait -PassThru -RedirectStandardOutput ${quote(snapshotFile)}; if ($process.ExitCode -ne 0) { throw 'Installed data service failed' }`);
  assert.equal(JSON.parse(fs.readFileSync(snapshotFile, 'utf8')).version, version);
  // The installed executable must repair and back up a profile on its own,
  // without invoking a development Go/Python/Node runtime.
  const database = path.join(profile, 'monitor.sqlite');
  const originalDigest = digest(database);
  run(path.join(destination, exe), ['--repair-database', '--data', profile]);
  const backups = path.join(profile, 'database-backups');
  const backupDirectories = fs.readdirSync(backups);
  assert.equal(backupDirectories.length, 1, 'Repair must retain the original database');
  assert.equal(digest(path.join(backups, backupDirectories[0], 'monitor.sqlite')), originalDigest);
  const running = spawn(path.join(destination, exe), ['--offline', '--stdio', '--data', profile, '--home', profile], { stdio: ['pipe', 'ignore', 'pipe'], windowsHide: true });
  await once(running, 'spawn');
  try {
    for (const [file, args] of [[candidate, ['/S', `/D=${destination}`]], [uninstaller, ['/S', `_?=${destination}`]]]) {
      const blocked = spawnSync(file, args, { windowsVerbatimArguments: true, argv0: `"${file}"`, windowsHide: true, timeout: 30000 });
      if (blocked.error) throw blocked.error;
      assert.equal(blocked.status, 2, 'Must refuse to replace a running application');
    }
  } finally {
    const stopped = once(running, 'exit');
    running.stdin.end();
    await stopped;
  }
  powershell(`New-ItemProperty -LiteralPath 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run' -Name ${quote(name)} -Value ${quote(`"${path.join(destination, exe)}" --mygo-opened-at-login`)} -PropertyType String -Force | Out-Null`);
  // Installing again upgrades the same directory and keeps user-added files.
  runInstaller(candidate, ['/S', `/D=${destination}`]);
  validateFiles();
  runInstaller(uninstaller, ['/S', `_?=${destination}`]);
  installed = false;
  for (const relative of files(bundle)) {
    assert.ok(!fs.existsSync(path.join(destination, relative === 'Codex Monitor Native.exe' ? exe : relative)), `Uninstall retained ${relative}`);
  }
  assert.ok(fs.existsSync(path.join(destination, 'keep-user-file.txt')));
  assert.ok(fs.existsSync(path.join(destination, 'resources', 'keep-user-file.txt')));
  assert.ok(fs.existsSync(path.join(destination, 'widget', 'keep-user-file.txt')));
  assert.equal(powershell(`Test-Path -LiteralPath ${quote(registry)}`).trim(), 'False');
  assert.equal(powershell(`$key = Get-Item -LiteralPath 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Run'; [bool]$key.GetValue(${quote(name)}, $null)`).trim(), 'False');
  for (const directory of Object.values(shortcutPaths)) assert.ok(!fs.existsSync(path.join(directory, `${name}.lnk`)));
  console.log(`Installer acceptance passed: ${count} file hashes, shortcuts, registry, installed service and database repair, running-app guard, upgrade and uninstall. User-added files preserved.`);
} finally {
  for (const directory of Object.values(shortcutPaths)) fs.rmSync(path.join(directory, `${legacyName}.lnk`), { force: true });
  if (installed && fs.existsSync(uninstaller)) runInstaller(uninstaller, ['/S', `_?=${destination}`]);
  const resolved = path.resolve(temporary);
  if (!resolved.startsWith(artifacts + path.sep)) throw new Error('Refusing to clean outside artifacts.');
  fs.rmSync(resolved, { recursive: true });
}
}
main().catch(error => { console.error(error); process.exitCode = 1; });
