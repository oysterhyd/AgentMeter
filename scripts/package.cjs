const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const version = require('../package.json').version;
const bundle = path.join(root, 'build', 'native');
const installer = path.join(root, 'release', `Codex-Monitor-Setup-${version}.exe`);

function files(directory, prefix = '') {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const relative = path.join(prefix, entry.name);
    if (entry.isDirectory()) return files(path.join(directory, entry.name), relative);
    if (!entry.isFile()) throw new Error(`Unexpected bundle entry: ${relative}`);
    return [relative];
  }).sort();
}

function findCached(cacheName, match) {
  const cache = path.join(process.env.LOCALAPPDATA || '', 'electron-builder', 'Cache', cacheName);
  if (!fs.existsSync(cache)) return;
  return files(cache).map(file => path.join(cache, file)).find(match);
}

function compilerPath() {
  if (process.env.MAKENSIS) return process.env.MAKENSIS;
  const onPath = spawnSync('where.exe', ['makensis.exe'], { encoding: 'utf8', windowsHide: true });
  if (onPath.status === 0) return onPath.stdout.trim().split(/\r?\n/)[0];
  const cached = findCached('nsis-3.0.4.1', file => /[\\/]makensis\.exe$/i.test(file) && !/[\\/]Bin[\\/]/i.test(file));
  if (cached) return cached;
  throw new Error('Install NSIS 3 and set MAKENSIS to makensis.exe, or add it to PATH.');
}

function pluginPath(compiler) {
  const candidates = [
    process.env.NSIS_PLUGIN_DIR,
    path.join(path.dirname(compiler), 'Plugins', 'x86-unicode'),
    path.join(path.dirname(compiler), '..', 'Plugins', 'x86-unicode'),
  ].filter(Boolean);
  const installed = candidates.find(directory => fs.existsSync(path.join(directory, 'nsProcess.dll')));
  if (installed) return installed;
  const cached = findCached('nsis-resources-3.4.1', file => /[\\/]x86-unicode[\\/]nsProcess\.dll$/i.test(file));
  if (cached) return path.dirname(cached);
  throw new Error('Install the Unicode nsProcess plugin or set NSIS_PLUGIN_DIR to its directory.');
}

function nsisPath(value) {
  if (/["$\r\n]/.test(value)) throw new Error(`Unsupported character in packaging path: ${value}`);
  return value.replaceAll('/', '\\');
}

function compile(options = {}) {
  if (process.platform !== 'win32' || process.arch !== 'x64') throw new Error('Package on Windows x64.');
  const compiler = compilerPath();
  const dictionary = options.dictionary || 16;
  if (![8, 16, 32].includes(dictionary)) throw new Error('Unsupported LZMA dictionary size.');
  const sourceFiles = files(bundle);
  for (const required of ['Codex Monitor Native.exe', 'icon.ico']) {
    if (!sourceFiles.includes(required)) throw new Error(`Incomplete native bundle: ${required}`);
  }
  if (sourceFiles.length !== 2) throw new Error('Unexpected files in the native-only bundle.');
  const output = options.output || installer;
  const manifests = path.join(root, 'artifacts', 'nsis');
  fs.mkdirSync(manifests, { recursive: true });
  fs.mkdirSync(path.dirname(output), { recursive: true });
  const uninstallManifest = path.join(manifests, 'uninstall-files.nsh');
  const directories = new Set();
  const lines = sourceFiles.map(file => {
    for (let parent = path.dirname(file); parent !== '.'; parent = path.dirname(parent)) directories.add(parent);
    return file === 'Codex Monitor Native.exe' ? 'Delete "$INSTDIR\\${APP_EXE}"' : `Delete "$INSTDIR\\${nsisPath(file)}"`;
  });
  lines.push(...[...directories].sort((a, b) => b.length - a.length).map(directory => `RMDir "$INSTDIR\\${nsisPath(directory)}"`));
  fs.writeFileSync(uninstallManifest, '\uFEFF' + lines.join('\n') + '\n');
  const legacyManifest = path.join(__dirname, 'legacy-app-files.nsh');
  const definitions = {
    APP_NAME: options.name || 'Codex Monitor',
    APP_EXE: options.exe || 'Codex Monitor Native.exe',
    APP_UNINSTALL_ID: options.uninstallId || '7eb26a54-5515-59fa-b127-ae9477be5900',
    APP_LOGIN_ID: options.loginId || 'Codex Monitor Native',
    APP_VERSION: version,
    APP_COMPRESSION_DICTIONARY: dictionary,
    APP_SIZE_KB: Math.ceil(sourceFiles.reduce((sum, file) => sum + fs.statSync(path.join(bundle, file)).size, 0) / 1024),
    BUNDLE_DIR: bundle,
    INSTALLER_FILE: output,
    PROCESS_PLUGIN_DIR: pluginPath(compiler),
    UNINSTALL_MANIFEST: uninstallManifest,
    LEGACY_MANIFEST: legacyManifest,
    LEGACY_WIDGET_MANIFEST: path.join(__dirname, 'legacy-widget-files.nsh'),
  };
  const args = ['/V2', '/INPUTCHARSET', 'UTF8', ...Object.entries(definitions).map(([key, value]) => `/D${key}=${nsisPath(String(value))}`), path.join(__dirname, 'installer.nsi')];
  const result = spawnSync(compiler, args, { cwd: root, stdio: 'inherit', windowsHide: true });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`NSIS compilation failed (${result.status}).`);
  return output;
}

if (require.main === module) {
  const build = spawnSync(process.execPath, [path.join(__dirname, 'native.cjs'), 'build'], { cwd: root, stdio: 'inherit', windowsHide: true });
  if (build.error) throw build.error;
  if (build.status !== 0) process.exit(build.status || 1);
  const output = compile();
  const digest = crypto.createHash('sha256').update(fs.readFileSync(output)).digest('hex');
  const updates = require('./updates.cjs').signedUpdate(bundle, path.dirname(output));
  const hashes = [output, ...updates].map(file => `${crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')}  ${path.basename(file)}`);
  fs.writeFileSync(path.join(root, 'release', `SHA256SUMS-${version}.txt`), hashes.join('\n') + '\n');
  console.log(`Installer: ${output}\nSHA-256: ${digest}`);
}

module.exports = { compile, files, bundle, installer, version };
