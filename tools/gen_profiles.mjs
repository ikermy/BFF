// Generates BFF revision profiles from the authoritative matrix
// (_TZ/new/BFF_ПОЛЯ_И_ЦЕПОЧКИ_ПО_РЕВИЗИЯМ.md).
//
// Usage:
//   node tools/gen_profiles.mjs --dry            # print parsed summary
//   node tools/gen_profiles.mjs --out DIR        # write YAML profiles to DIR
//   node tools/gen_profiles.mjs --render-fields FILE   # write renderFields JSON
import fs from 'node:fs';
import path from 'node:path';

const MATRIX = 'D:/prod/barcode-system/_TZ/new/BFF_ПОЛЯ_И_ЦЕПОЧКИ_ПО_РЕВИЗИЯМ.md';
const SERVICE = 'D:/prod/barcode-system/barcode_gen/src/barcode-config/barcode-config.service.ts';

const args = process.argv.slice(2);
const dry = args.includes('--dry');
const outIdx = args.indexOf('--out');
const outDir = outIdx >= 0 ? args[outIdx + 1] : null;
const rfIdx = args.indexOf('--render-fields');
const rfFile = rfIdx >= 0 ? args[rfIdx + 1] : null;
const fxIdx = args.indexOf('--fixtures');
const fxFile = fxIdx >= 0 ? args[fxIdx + 1] : null;

const doc = fs.readFileSync(MATRIX, 'utf8');
const lines = doc.split(/\r?\n/);

// ── parse profile sections ────────────────────────────────────────────────
const sections = [];
for (let i = 0; i < lines.length; i++) {
  const m = lines[i].match(/^### (\d+)\.\s+(.+?)\s+—\s+`([^`]+)`\s*$/);
  if (m) sections.push({ num: +m[1], title: m[2], id: m[3], start: i });
}
sections.forEach((s, i) => { s.end = i + 1 < sections.length ? sections[i + 1].start : lines.length; });

const CODE = '[A-Z][A-Z0-9]{1,2}';
const strip = (t) => t.replace(/\s*\([^)]*\)/g, '').trim();

function parseCodes(raw) {
  return raw.split(/[,+]/).map(x => strip(x)).filter(x => new RegExp(`^${CODE}$`).test(x));
}

function parseChain(text) {
  // text begins with "**Цепочка:**" possibly; split on →
  const steps = [];
  const parts = text.split('→');
  for (const part of parts) {
    const seg = part.trim().replace(/^`|`$/g, '');
    const m = seg.match(/^(random|calculate)(?:\s+\w+)?\[([^\]]*)\]\s*=>\s*(.+)$/);
    if (!m) continue;
    steps.push({ endpoint: m[1], input: parseCodes(m[2]), output: parseCodes(m[3]) });
  }
  return steps;
}

function parseProfile(s) {
  const body = lines.slice(s.start, s.end).join('\n');
  const p = { num: s.num, title: s.title, id: s.id, selects: {}, ignored: [], chain: [], caps: {}, county: false, safeDriver: false, notes: [] };

  const pr = body.match(/\*\*Профиль:\*\*\s*([^\n]+)/);
  if (pr) {
    const t = pr[1];
    const g = (k) => { const m = t.match(new RegExp('`' + k + '`[^=]*=\\s*`([^`]+)`')); return m ? m[1] : null; };
    p.DAJ = g('DAJ'); p.DDB = g('DDB'); p.QQQ = g('QQQ');
    p.country = /country\s*=\s*`CA`/.test(t) ? 'CA' : 'USA';
    const eng = t.match(/engine country\s*=\s*`([^`]+)`/);
    if (eng) p.engineCountry = eng[1];
  }

  const sel = body.match(/\*\*Выбор:\*\*\s*([^\n]+)/);
  if (sel) {
    for (const item of sel[1].split(';')) {
      const m = item.match(/`(D[A-Z]{2})`[^=]*=\s*`([^`]*)`(?:\s*,\s*fallback\s*`([^`]*)`)?/);
      if (m) p.selects[m[1]] = { options: m[2].split('|').map(x => x.trim()).filter(Boolean), fallback: m[3] !== undefined ? m[3] : (m[2].split('|')[0] || '').trim() };
    }
  }

  const ign = body.match(/\*\*Игнорировать:\*\*\s*([^\n]+)/);
  if (ign) p.ignored = parseCodes(ign[1].replace(/\s*\([^)]*\)/g, (x) => x)); // keep codes only
  // ре-парсим игнор аккуратно (коды в начале каждого пункта)
  if (ign) {
    p.ignored = [];
    for (const chunk of ign[1].split(',')) {
      const m = chunk.trim().match(/^`?(D[A-Z]{2})`?/);
      if (m) p.ignored.push(m[1]);
    }
  }

  const ch = body.match(/\*\*Цепочка:\*\*\s*([^\n`]*`[^\n]+)/);
  if (ch) p.chain = parseChain(ch[1]);

  // Canada prepared/manual chain
  const prep = body.match(/\*\*Prepared\/manual цепочка:\*\*\s*([^\n]+)/);
  if (prep) p.preparedChain = true;

  if (/\*\*Дополнительн[^\n]*county/i.test(body) || /\bcounty\b/.test(body) && /→\s*`?ZGD/.test(body)) p.county = true;
  if (/`safeDriver`/.test(body)) p.safeDriver = true;

  p.cap = {
    DAH: /addressLine2`?\s*→\s*`?DAH/.test(body),
    DCU: /`suffix`\s*→\s*`?DCU/.test(body),
  };

  const st = body.match(/\*\*Статус:\*\*\s*([^\n]+)/);
  if (st) p.status = st[1];

  return p;
}

const profiles = sections.map(parseProfile);
if (dry) {
  console.log('profiles parsed:', profiles.length);
  for (const p of profiles.slice(0, 12)) {
    console.log(`#${p.num} ${p.id} DAJ=${p.DAJ} DDB=${p.DDB} QQQ=${p.QQQ} country=${p.country} steps=${p.chain.length} selects=${Object.keys(p.selects).join(',')} ignored=${p.ignored.join(',')}${p.county ? ' county' : ''}${p.safeDriver ? ' safeDriver' : ''}`);
    for (const st of p.chain) console.log(`    ${st.endpoint}[${st.input.join(',')}] => ${st.output.join('+')}`);
  }
  const noChain = profiles.filter(p => p.chain.length === 0);
  console.log('no-chain profiles:', noChain.map(p => p.id).join(', ') || '(none)');
  process.exit(0);
}

// ── builder read-sets from barcode_gen ────────────────────────────────────
const svc = fs.readFileSync(SERVICE, 'utf8').split(/\r?\n/);
const stateRe = /state:\s*'([^']+)'/;
const revRe = /rev:\s*'([^']+)'/;
const readSet = {}; // DAJ|DDB -> Set(codes)
{
  let cur = null, grabbing = false, depth = 0;
  for (let i = 0; i < svc.length; i++) {
    const sm = svc[i].match(stateRe);
    const rm = svc[i].match(revRe);
    if (sm) cur = { state: sm[1], rev: null, codes: new Set() , key:null};
    if (rm && cur) { cur.rev = rm[1]; cur.key = `${cur.state}|${cur.rev}`; readSet[cur.key] = cur.codes; }
    if (/generate\(data/.test(svc[i]) && cur) {
      // read forward ~120 lines until the closing of generate
      let d = 0, started = false;
      for (let j = i; j < Math.min(i + 140, svc.length); j++) {
        const re = /data\.([A-Z]{3})/g; let m;
        while ((m = re.exec(svc[j]))) cur.codes.add(m[1]);
        for (const ch of svc[j]) { if (ch === '{') { d++; started = true; } else if (ch === '}') d--; }
        if (started && d <= 0) break;
      }
    }
  }
}

// ── step guards (internal ops) ────────────────────────────────────────────
function guards(endpoint, output, p) {
  const key = [...output].sort().join(',');
  if (endpoint === 'random') {
    if (key === 'DAQ') return ['DAJ'];
    if (key === 'DBA,DBD') return p.DAJ === 'AZ' || p.DAJ === 'CO' ? ['DAJ', 'DBB', 'DDB', 'DDA'] : ['DAJ', 'DBB', 'DDB'];
    if (key === 'DCJ') return ['DAJ', 'DBD'];
    return ['DAJ'];
  }
  if (key === 'DBA') return ['DBD'];
  if (key === 'DCK') return ['DBD', 'DAQ', 'DBA', 'DBB'];
  return [];
}

const BASE_FIELDS = {
  DAC: 'firstName', DAD: 'middleName', DCS: 'lastName', DAG: 'street', DAH: 'addressLine2',
  DAI: 'city', DAK: 'zipCode', DBB: 'dateOfBirth', DBC: 'sex', DDL: 'veteran', DDK: 'organDonor',
  DDA: 'realId', DAU: 'height', DAW: 'weightPounds', DAY: 'eyeColor', DAZ: 'hairColor',
  DCL: 'race', DCU: 'suffix', DCA: 'vehicleClass', DCB: 'restrictions', DCD: 'endorsements',
};
const EYE = ['BLK', 'BLU', 'BRO', 'DIC', 'GRY', 'GRN', 'HAZ', 'MAR', 'PNK', 'UNK'];
const HAIR = ['BAL', 'RED', 'BLK', 'SDY', 'BLN', 'WHI', 'BRO', 'GRY', 'UNK'];
const HAIR_NV2008 = ['BALD', 'RED', 'BLACK', 'SANDY', 'BLOND', 'WHITE', 'BROWN', 'GRAY', 'UNKNOWN'];
const RACE = ['AI', 'AP', 'BK', 'H', 'O', 'W', 'U'];
const SUFFIX = ['', 'JR', 'SR', '1ST', '2ND', '3RD', '4TH', '5TH', '6TH', '7TH', '8TH', '9TH'];
const KNOWN = new Set(['US_CA_08292017', 'US_WA_11122019', 'US_CO_10302015', 'US_LA_02102015', 'US_MI_Rev_01-21-2011', 'US_OH_07012018']);

function effDate(p) {
  if (p.DDB && /^Rev /.test(p.DDB)) return '2011-01-21';
  const d = (p.DDB || '').match(/^(\d{2})(\d{2})(\d{4})$/);
  return d ? `${d[3]}-${d[1]}-${d[2]}` : '';
}

function yamlField(f) {
  const parts = [`name: ${f.name}`, `type: ${f.type}`, `required: ${f.required}`, `label: "${f.label}"`, `order: ${f.order}`];
  if (f.options) parts.push(`options: [${f.options.map(o => JSON.stringify(o)).join(', ')}]`);
  if (f.validation) parts.push(`validation: {${f.validation}}`);
  return `    - {${parts.join(', ')}}`;
}

function renderYaml(p) {
  const codes = readSet[`${p.DAJ}|${p.DDB}`] || new Set();
  const chainCodes = new Set(['DAJ', 'DDB', 'QQQ']);
  for (const st of p.chain) { st.input.forEach(c => chainCodes.add(c)); st.output.forEach(c => chainCodes.add(c)); }
  for (const k of Object.keys(p.selects)) chainCodes.add(k);

  const ignored = new Set(p.ignored);
  const allow = new Set();
  for (const c of chainCodes) if (!ignored.has(c)) allow.add(c);
  // add base fields that builder reads and profile does not ignore
  for (const c of codes) if (!ignored.has(c) && BASE_FIELDS[c]) allow.add(c);
  // fallbacks for selects
  const defaults = {};
  defaults.QQQ = p.QQQ;
  for (const [c, s] of Object.entries(p.selects)) {
    if (!ignored.has(c) && s.fallback !== undefined && s.fallback !== '') defaults[c] = s.fallback;
  }
  if (!ignored.has('DBC')) defaults.DBC = '1';
  if (!ignored.has('DDA')) defaults.DDA = p.selects.DDA?.fallback ?? 'F';
  if (!ignored.has('DCL') && allow.has('DCL')) defaults.DCL = 'U';
  // OH/AB builder использует internal-слот DAX (weight kg); разрешаем его в allow.
  if (p.DAJ === 'OH' || p.DAJ === 'AB') allow.add('DAX');
  // Некоторые builders требуют присутствия ключа DAD (пустая строка допустима).
  allow.add('DAD');
  defaults.DAD = '';
  for (const c of Object.keys(defaults)) if (!allow.has(c)) delete defaults[c];

  // schema
  const canadaFlag = p.country === 'CA';
  const fields = [];
  let order = 1;
  const reqCore = new Set(['firstName', 'lastName', 'dateOfBirth']);
  if (p.id.startsWith('US_LA')) reqCore.add('auditCode');
  if (canadaFlag) {
    reqCore.add('issueDate'); reqCore.add('expirationDate');
    reqCore.add('dlNumber'); reqCore.add('documentDiscriminator');
    if (p.DAJ === 'ON') reqCore.add('inventoryNumber');
  }
  // requiredInputFields по доку: обязательные semantic-поля, присутствующие в профиле.
  const reqFull = new Set(reqCore);
  for (const [code, pub] of [['DAG', 'street'], ['DAI', 'city'], ['DAK', 'zipCode'],
    ['DAY', 'eyeColor'], ['DAZ', 'hairColor'], ['DAU', 'height'], ['DAW', 'weightPounds']]) {
    if (allow.has(code)) reqFull.add(pub);
  }
  if (p.county) reqFull.add('county');
  const push = (name, type, required, label, options, validation) => fields.push({ name, type, required, label, order: order++, options, validation });
  push('firstName', 'string', true, 'First Name');
  if (allow.has('DAD')) push('middleName', 'string', false, 'Middle Name');
  push('lastName', 'string', true, 'Last Name');
  if (allow.has('DCU')) push('suffix', 'enum', false, 'Suffix', SUFFIX);
  if (allow.has('DAG')) push('street', 'string', true, 'Street');
  if (allow.has('DAH')) push('addressLine2', 'string', false, 'Address line 2');
  if (allow.has('DAI')) push('city', 'string', true, 'City');
  if (allow.has('DAK')) push('zipCode', 'string', true, 'ZIP/Postal');
  push('dateOfBirth', 'date', true, 'Date of Birth', null, 'maxDate: "today-16y"');
  if (allow.has('DBC')) push('sex', 'enum', false, 'Sex', ['1', '2']);
  if (allow.has('DDA')) push('realId', 'enum', false, 'Real ID', ['', 'N', 'F']);
  if (allow.has('DCL')) push('race', 'enum', false, 'Race', RACE);
  if (allow.has('DCA')) push('vehicleClass', 'enum', false, 'Class', p.selects.DCA?.options || []);
  if (allow.has('DCB')) push('restrictions', 'enum', false, 'Restrictions', p.selects.DCB?.options || []);
  if (allow.has('DCD')) push('endorsements', 'enum', false, 'Endorsements', p.selects.DCD?.options || []);
  if (allow.has('DAU')) push('height', 'number', false, 'Height (in)');
  if (allow.has('DAW')) push('weightPounds', 'number', false, 'Weight (lbs)');
  if (allow.has('DAY')) push('eyeColor', 'enum', false, 'Eye Color', EYE);
  if (allow.has('DAZ')) push('hairColor', 'enum', false, 'Hair Color', p.selects.DAZ?.options || (p.DAJ === 'NV' && p.DDB === '10102008' ? HAIR_NV2008 : HAIR));
  if (allow.has('DDK')) push('organDonor', 'enum', false, 'Organ Donor', ['false', 'true']);
  if (allow.has('DDL')) push('veteran', 'enum', false, 'Veteran', ['false', 'true']);
  if (p.county) push('county', 'string', true, 'County');
  if (p.safeDriver) push('safeDriver', 'enum', false, 'Safe Driver', ['false', 'true']);
  if (p.id.startsWith('US_LA')) push('auditCode', 'string', true, 'Audit Information', null, 'maxLength: 64');
  if (p.id.startsWith('US_WA') || p.id.startsWith('US_CO')) push('auditCode', 'string', false, 'Audit Information');
  if (canadaFlag) {
    for (const c of ['DBD', 'DBA', 'DAQ', 'DCF', ...(p.DAJ === 'ON' ? ['DCK'] : [])]) allow.add(c);
    push('issueDate', 'date', true, 'Issue Date');
    push('expirationDate', 'date', true, 'Expiration Date');
    // prepared/manual: проверенные значения предоставляет UI/draft (док ON/AB).
    push('dlNumber', 'string', true, 'Document Number');
    push('documentDiscriminator', 'string', true, 'Document Discriminator');
    if (p.DAJ === 'ON') push('inventoryNumber', 'string', true, 'Inventory Number');
  }

  const laterOutputs = (i) => {
    const s = new Set();
    for (let j = i; j < p.chain.length; j++) p.chain[j].output.forEach(c => s.add(c));
    return s;
  };
  const stepData = p.chain.map((st, i) => {
    // Внутренние helper'ы BarcodeGen считают output через builder.generate(),
    // который читает весь профильный набор полей. Поэтому derive-input = union
    // (matrix deps ∪ guards ∪ builder read-set), КРОМЕ полей, производимых текущим
    // или более поздним шагом (ordered reachability).
    const forbidden = laterOutputs(i);
    const input = [...new Set([...st.input, ...guards(st.endpoint, st.output, p), ...codes, 'DAJ', 'DDB'])].filter(c => !ignored.has(c) && !forbidden.has(c));
    return { id: `s${i}_${st.endpoint}_${st.output.join('').toLowerCase()}`, endpoint: st.endpoint, input, output: st.output };
  });
  const steps = stepData.map(sd =>
    `  - id: ${sd.id}\n    endpoint: ${sd.endpoint}\n    input: [${sd.input.join(', ')}]\n    output: [${sd.output.join(', ')}]`,
  );
  p._fixture = {
    name: p.id,
    country: p.country,
    heightCm: !!canadaFlag,
    revisionEffectiveDate: effDate(p),
    supportedModes: canadaFlag ? ['prepare', 'prepared'] : ['prepare', 'auto', 'prepared'],
    requiredInputFields: [...reqFull].sort(),
    renderAllowlist: [...allow].sort(),
    steps: stepData,
  };

  const modes = canadaFlag ? '[prepare, prepared]' : '[prepare, auto, prepared]';
  p._allow = [...allow];
  const y = [];
  y.push(`name: ${p.id}`);
  y.push(`displayName: "${p.title}"`);
  y.push(`enabled: true`);
  y.push(`revisionEffectiveDate: "${effDate(p)}"`);
  y.push(`country: ${p.country}`);
  if (canadaFlag) y.push(`heightCm: true`);
  y.push(`supportedModes: ${modes}`);
  y.push(`renderAllowlist: [${[...allow].join(', ')}]`);
  if (Object.keys(defaults).length) {
    y.push('defaults:');
    for (const [k, v] of Object.entries(defaults)) y.push(`  ${k}: ${JSON.stringify(String(v))}`);
  }
  y.push(`requiredInputFields: [${[...reqFull].join(', ')}]`);
  y.push(`baseInput: [${[...reqCore].join(', ')}]`);
  if (steps.length) { y.push('generationSteps:'); y.push(steps.join('\n')); }
  else y.push('generationSteps: []');
  y.push('schema:');
  y.push('  fields:');
  for (const f of fields) y.push(yamlField(f));
  return y.join('\n') + '\n';
}

if (outDir) {
  fs.mkdirSync(outDir, { recursive: true });
  let written = 0;
  for (const p of profiles) {
    const yaml = renderYaml(p); // всегда считаем allow (для render-fields), даже для KNOWN
    if (KNOWN.has(p.id)) continue; // keep hand-tuned existing 6
    fs.writeFileSync(path.join(outDir, `${p.id}.yaml`), yaml);
    written++;
  }
  console.log('written profiles:', written);
}

if (rfFile) {
  const map = {};
  for (const p of profiles) {
    const codes = readSet[`${p.DAJ}|${p.DDB}`];
    map[p.id] = { DAJ: p.DAJ, DDB: p.DDB, QQQ: p.QQQ, codes: codes ? [...codes].sort() : [], allow: p._allow || [] };
  }
  fs.writeFileSync(rfFile, JSON.stringify(map, null, 2));
  console.log('render-fields written:', rfFile);
}

if (fxFile) {
  for (const p of profiles) if (!p._fixture) renderYaml(p);
  const arr = profiles.filter(p => !KNOWN.has(p.id)).map(p => p._fixture).filter(Boolean).sort((a, b) => a.name.localeCompare(b.name));
  fs.writeFileSync(fxFile, JSON.stringify({ profiles: arr }, null, 2));
  console.log('fixtures written:', arr.length, fxFile);
}
