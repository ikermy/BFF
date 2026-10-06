// Line-based, idempotent: вставляет renderFields после строки country: для каждого
// config-блока (state/rev/country в любом порядке). Источник — render_fields.json.
import fs from 'node:fs';

const RF = 'D:/prod/barcode-system/bff/tools/render_fields.json';
const SVC = 'D:/prod/barcode-system/barcode_gen/src/barcode-config/barcode-config.service.ts';

const rf = JSON.parse(fs.readFileSync(RF, 'utf8'));
const byKey = {};
for (const m of Object.values(rf)) {
  const key = `${m.DAJ}|${m.DDB}`;
  const set = byKey[key] || (byKey[key] = new Set());
  for (const c of m.allow || []) set.add(c);
  for (const c of m.codes || []) set.add(c);
  for (const c of ['DAJ', 'DDB', 'QQQ']) set.add(c);
}
for (const k of Object.keys(byKey)) byKey[k] = [...byKey[k]].sort().map(c => `'${c}'`).join(', ');

const src = fs.readFileSync(SVC, 'utf8');
const eol = src.includes('\r\n') ? '\r\n' : '\n';
const lines = src.split(/\r?\n/);
const out = [];
let patched = 0;

for (let i = 0; i < lines.length; i++) {
  const sm = lines[i].match(/^(\s*)state: '([A-Z]{2})',\s*$/);
  if (!sm) { out.push(lines[i]); continue; }

  const indent = sm[1];
  let rev = null, countryIdx = -1;
  for (let j = i + 1; j <= i + 2 && j < lines.length; j++) {
    const rm = lines[j].match(/^\s*rev: '([^']+)',\s*$/);
    if (rm) rev = rm[1];
    if (/^\s*country: '/.test(lines[j])) countryIdx = j;
  }
  if (!rev || countryIdx < 0) { out.push(lines[i]); continue; }

  const list = byKey[`${sm[2]}|${rev}`];
  for (let j = i; j <= countryIdx; j++) out.push(lines[j]);
  if (list) {
    out.push(`${indent}renderFields: [${list}],`);
    patched++;
  }
  const next = countryIdx + 1;
  const hasOld = list && (lines[next] || '').trimStart().startsWith('renderFields:');
  i = hasOld ? next : countryIdx;
}

fs.writeFileSync(SVC, out.join(eol));
console.log('patched:', patched);
