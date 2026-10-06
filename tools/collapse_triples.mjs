import fs from 'node:fs';
const p = 'D:/prod/barcode-system/barcode_gen/src/barcode-config/barcode-config.service.ts';
let t = fs.readFileSync(p, 'utf8');
const reUS = /(state: '([^'\n]*)',\r?\n\s*rev: '([^'\n]*)',\r?\n\s*country: '([^'\n]*)',\r?\n)(?:\s*state: '\2',\r?\n\s*rev: '\3',\r?\n\s*country: '\4',\r?\n)+/g;
const reCA = /(state: '([^'\n]*)',\r?\n\s*country: '([^'\n]*)',\r?\n\s*rev: '([^'\n]*)',\r?\n)(?:\s*state: '\2',\r?\n\s*country: '\3',\r?\n\s*rev: '\4',\r?\n)+/g;
t = t.replace(reUS, '$1').replace(reCA, '$1');
fs.writeFileSync(p, t);
console.log('state keys:', (t.match(/state: '[A-Z]{2}'/g) || []).length, 'generate:', (t.match(/generate\(data/g) || []).length);
