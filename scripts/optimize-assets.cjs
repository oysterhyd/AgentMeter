// Recompress PNG IDAT streams without changing a scanline, pixel or metadata.
const fs = require('node:fs');
const crypto = require('node:crypto');
const zlib = require('node:zlib');
const path = require('node:path');
const root = path.resolve(__dirname, '..');

for (const name of ['icon.png', 'monitor-glass.png']) {
  const file = path.join(root, 'assets', name);
  const before = fs.readFileSync(file);
  if (!before.subarray(0, 8).equals(Buffer.from('89504e470d0a1a0a', 'hex'))) throw new Error(`Invalid PNG: ${name}`);
  const chunks = [], data = [];
  for (let offset = 8; offset < before.length;) {
    const length = before.readUInt32BE(offset), end = offset + length + 12;
    if (end > before.length) throw new Error(`Truncated PNG: ${name}`);
    const chunk = before.subarray(offset, end);
    const idat = chunk.toString('ascii', 4, 8) === 'IDAT';
    chunks.push({ chunk, idat });
    if (idat) data.push(chunk.subarray(8, -4));
    offset = end;
  }
  const scanlines = zlib.inflateSync(Buffer.concat(data));
  const compressed = zlib.deflateSync(scanlines, { level: 9 });
  const idat = Buffer.alloc(compressed.length + 12);
  idat.writeUInt32BE(compressed.length);
  idat.write('IDAT', 4, 'ascii');
  compressed.copy(idat, 8);
  idat.writeUInt32BE(zlib.crc32(idat.subarray(4, -4)), idat.length - 4);
  let inserted = false;
  const after = Buffer.concat([before.subarray(0, 8), ...chunks.flatMap(({ chunk, idat: isData }) => {
    if (!isData) return [chunk];
    if (inserted) return [];
    inserted = true;
    return [idat];
  })]);
  if (!zlib.inflateSync(compressed).equals(scanlines)) throw new Error(`Scanlines changed: ${name}`);
  if (after.length < before.length) fs.writeFileSync(file, after);
  console.log(JSON.stringify({ file: name, before: before.length, after: Math.min(before.length, after.length), scanlineSHA256: crypto.createHash('sha256').update(scanlines).digest('hex') }));
}
