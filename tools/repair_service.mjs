import fs from 'node:fs';
const p = 'D:/prod/barcode-system/barcode_gen/src/barcode-config/barcode-config.service.ts';
let t = fs.readFileSync(p, 'utf8');

// 1) удалить все renderFields-массивы (вставим заново корректно)
t = t.replace(/renderFields:\s*\[[^\]]*\],?/g, '');

// 2) разбить склеенные ключи конфигурации на отдельные строки
t = t.replace(/,[ \t]*(state: '|rev: '|country: '|fields: \{)/g, ',\n      $1');

// 3) схлопнуть повторяющиеся подряд тройки state/rev/country
const triple = /(state: '[^']*',\n\s*rev: '[^']*',\n\s*country: '[^']*',)\n\s*\1\n/g;
let prev;
do { prev = t; t = t.replace(triple, '$1\n'); } while (t !== prev);

// 4) почистить возможные пустые строки с хвостовыми пробелами после country
t = t.replace(/,\n\s*\n(?=\s*(?:state:|fields:))/g, ',\n');

fs.writeFileSync(p, t);
const states = (t.match(/state: '[A-Z]{2}'/g) || []).length;
const rfs = (t.match(/renderFields:/g) || []).length;
console.log('state keys:', states, 'renderFields:', rfs);
