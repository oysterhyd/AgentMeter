// MyGO's signed archive format, applied to our existing two-file NSIS bundle.
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const config = require('../mygo.json');

function signedUpdate(bundle, release) {
  const keyFile = process.env.MYGO_UPDATER_KEY_FILE || path.join(process.env.LOCALAPPDATA || '', 'codex-monitor', 'update-keys', 'mygo-update.key');
  const keyText = process.env.MYGO_UPDATER_PRIVATE_KEY || (fs.existsSync(keyFile) ? fs.readFileSync(keyFile, 'utf8').trim() : '');
  if (!keyText) { console.log('No update signing key; generated the installer only.'); return []; }
  const raw = Buffer.from(keyText, 'base64');
  if (raw.length !== 64) throw new Error('Invalid MyGO signing key length.');
  const key = crypto.createPrivateKey({ key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), raw.subarray(0, 32)]), type: 'pkcs8', format: 'der' });
  const publicKey = crypto.createPublicKey(key).export({ type: 'spki', format: 'der' }).subarray(-32);
  if (!publicKey.equals(Buffer.from(config.updates.publicKey, 'base64'))) throw new Error('The update signing key does not match mygo.json.');
  const archive = path.join(release, `AgentMeter-${config.version}-windows-amd64.tar.gz`);
  // bsdtar reads arguments in the ANSI code page; relative paths keep a
  // non-ASCII checkout directory out of them.
  const result = spawnSync('tar.exe', ['--options', 'gzip:compression-level=9', '-czf', path.basename(archive), '-C', path.relative(release, bundle), 'Codex Monitor Native.exe', 'icon.ico'], { cwd: release, windowsHide: true, encoding: 'utf8' });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Update archive failed: ${result.stderr}`);
  const bytes = fs.readFileSync(archive);
  const digest = crypto.createHash('sha256').update(bytes).digest();
  const signature = crypto.sign(null, digest, key);
  if (!crypto.verify(null, digest, crypto.createPublicKey(key), signature)) throw new Error('Update signature verification failed.');
  const changelog = fs.readFileSync(path.join(__dirname, '..', 'CHANGELOG.md'), 'utf8').replaceAll('\r\n', '\n');
  const notes = changelog.split(`## ${config.version}\n`)[1]?.split('\n## ')[0]?.trim();
  if (!notes) throw new Error('Missing release notes for the packaged version.');
  const manifest = path.join(release, 'update-windows-amd64.json');
  fs.writeFileSync(manifest, JSON.stringify({ version: config.version, notes, date: new Date().toISOString(), url: `https://github.com/${config.updates.github}/releases/download/v${config.version}/${path.basename(archive)}`, size: bytes.length, signature: signature.toString('base64') }, null, 2) + '\n');
  return [archive, manifest];
}
module.exports = { signedUpdate };
