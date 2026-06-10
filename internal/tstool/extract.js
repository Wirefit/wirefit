#!/usr/bin/env node
// wirefit TypeScript extractor (Phase 2, PRD 2.2 + 2.3).
//
// Two extraction paths, auto-detected per export:
//   1. Types (interface / type alias / enum): resolved through the project's
//      own TypeScript compiler — never re-implements type logic (SPEC §6).
//   2. Zod schemas (exported const): the module is runtime-imported and the
//      SERVICE'S OWN zod (v4) emits JSON Schema via z.toJSONSchema, which is
//      then normalized to wirefit IR. Zod yields richer scalars than the type
//      system can (z.uuid() → uuid, z.iso.datetime() → datetime).
//
// usage: node extract.js --project <dir> (in=|out=)<relative/file.ts#Export>...
//   in=  : consumer side — zod io "input" (.default() fields are optional)
//   out= : provider side — zod io "output" (.default() fields are required)
//   bare : type-path only; role is irrelevant there
//
// output: one JSON object on stdout keyed by the bare spec.
// exit codes: 0 ok · 2 unsupported shape / resolution failure

'use strict';
const path = require('path');
const url = require('url');
const Module = require('module');
const ts = require(path.join(__dirname, 'node_modules', 'typescript'));

function die(msg) {
  console.error('wirefit-extract-ts: ' + msg);
  process.exit(2);
}

const argv = process.argv.slice(2);
let projectDir = process.cwd();
const specs = []; // { spec, file, name, role: 'in'|'out' }
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === '--project') { projectDir = path.resolve(argv[++i]); continue; }
  let role = 'in';
  let s = argv[i];
  if (s.startsWith('in=')) { s = s.slice(3); }
  else if (s.startsWith('out=')) { role = 'out'; s = s.slice(4); }
  const [file, name] = s.split('#');
  if (!name) die(`spec ${s} must be <file.ts>#<ExportName>`);
  specs.push({ spec: s, file, name, role });
}
if (specs.length === 0) die('usage: extract.js --project <dir> (in=|out=)<file.ts#Export>...');

// --- program setup: use the service's own tsconfig when present -----------
let options = {
  strict: true,
  target: ts.ScriptTarget.ES2022,
  module: ts.ModuleKind.NodeNext,
  moduleResolution: ts.ModuleResolutionKind.NodeNext,
  noEmit: true,
};
const configPath = ts.findConfigFile(projectDir, ts.sys.fileExists, 'tsconfig.json');
if (configPath) {
  const cfg = ts.readConfigFile(configPath, ts.sys.readFile);
  if (cfg.error) die('tsconfig: ' + ts.flattenDiagnosticMessageText(cfg.error.messageText, ' '));
  const parsed = ts.parseJsonConfigFileContent(cfg.config, ts.sys, path.dirname(configPath));
  options = { ...parsed.options, noEmit: true };
  if (options.strict === undefined && options.strictNullChecks === undefined) {
    die('tsconfig has neither strict nor strictNullChecks — nullability cannot be extracted');
  }
}

const rootFiles = [...new Set(specs.map((s) => path.resolve(projectDir, s.file)))];
const program = ts.createProgram(rootFiles, options);
const checker = program.getTypeChecker();

const syntactic = program.getSyntacticDiagnostics();
if (syntactic.length > 0) {
  die('syntax error: ' + ts.flattenDiagnosticMessageText(syntactic[0].messageText, ' '));
}

// --- dispatch: type declaration vs zod value export ------------------------
const moduleCache = new Map();

(async () => {
  const out = {};
  for (const entry of specs) {
    const sf = program.getSourceFile(path.resolve(projectDir, entry.file));
    if (!sf) die(`cannot load ${entry.file}`);

    let typeDecl = null;
    let varDecl = null;
    sf.forEachChild((node) => {
      if ((ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node) || ts.isEnumDeclaration(node)) &&
          node.name && node.name.text === entry.name) {
        typeDecl = node;
      }
      if (ts.isVariableStatement(node)) {
        for (const d of node.declarationList.declarations) {
          if (ts.isIdentifier(d.name) && d.name.text === entry.name) varDecl = d;
        }
      }
    });

    if (typeDecl) {
      const symbol = checker.getSymbolAtLocation(typeDecl.name);
      const type = checker.getDeclaredTypeOfSymbol(symbol);
      out[entry.spec] = schemaFor(type, [], `${entry.file}#${entry.name}`);
      continue;
    }
    if (varDecl) {
      const vt = checker.getTypeAtLocation(varDecl.name);
      const sym = vt.getSymbol && vt.getSymbol();
      if (sym && /^Zod/.test(sym.getName())) {
        out[entry.spec] = await extractZod(entry);
        continue;
      }
      die(`${entry.name} in ${entry.file} is a value of type "${checker.typeToString(vt)}" — ` +
          'only Zod schemas are supported as value exports');
    }
    die(`${entry.name} not found in ${entry.file} (must be a top-level interface, type alias, enum or exported Zod schema)`);
  }
  process.stdout.write(JSON.stringify(out, null, 2) + '\n');
})().catch((e) => die(e && e.stack ? e.stack : String(e)));

// === path 2: Zod ============================================================

async function extractZod(entry) {
  const abs = path.resolve(projectDir, entry.file);
  const ctx = `${entry.file}#${entry.name}`;
  if (!process.features.typescript) {
    die('runtime TypeScript import requires Node >= 22.6 with type stripping (for Zod schema extraction)');
  }
  let mod = moduleCache.get(abs);
  if (!mod) {
    try {
      mod = await import(url.pathToFileURL(abs).href);
    } catch (e) {
      die(`runtime import of ${entry.file} failed (Zod path executes the module; keep schema files dependency-light): ${e.message}`);
    }
    moduleCache.set(abs, mod);
  }
  const schema = mod[entry.name];
  if (!schema) die(`export ${entry.name} not found at runtime in ${entry.file}`);

  // Use the SERVICE'S zod — versions must agree with the schema objects.
  const req = Module.createRequire(abs);
  let zmod;
  try {
    zmod = await import(url.pathToFileURL(req.resolve('zod')).href);
  } catch (e) {
    die(`cannot resolve "zod" from ${entry.file}: ${e.message}`);
  }
  const z = zmod.z ?? zmod.default ?? zmod;
  if (typeof z.toJSONSchema !== 'function') {
    die('z.toJSONSchema not found — Zod v4+ is required (v3 schemas are not supported, PRD 2.3)');
  }
  let js;
  try {
    js = z.toJSONSchema(schema, { io: entry.role === 'out' ? 'output' : 'input' });
  } catch (e) {
    die(`z.toJSONSchema failed for ${ctx}: ${e.message} — ` +
        'unrepresentable types (z.date(), z.bigint(), transforms without output type) cannot be contract-checked; ' +
        'use z.iso.datetime() / z.uuid() / typed pipes instead');
  }
  return jsonSchemaToIR(js, js.$defs || {}, ctx, []);
}

const FORMAT_SCALARS = { uuid: 'uuid', 'date-time': 'datetime', date: 'date', duration: 'duration' };

// Normalizes the JSON Schema subset that z.toJSONSchema emits into wirefit IR.
function jsonSchemaToIR(js, defs, ctx, refStack) {
  if (js === true || js === undefined || js === null) {
    die(`unconstrained schema at ${ctx} (z.any()/z.unknown()) — give it a concrete type`);
  }
  if (js.$ref) {
    const m = /^#\/\$defs\/(.+)$/.exec(js.$ref);
    if (!m) die(`unsupported $ref ${js.$ref} at ${ctx}`);
    const key = decodeURIComponent(m[1]);
    if (refStack.includes(key)) return { 'x-ct-recursive': true };
    if (!(key in defs)) die(`missing $defs entry ${key} at ${ctx}`);
    return jsonSchemaToIR(defs[key], defs, ctx, [...refStack, key]);
  }

  // type: ["string","null"] form
  let node = { ...js };
  let nullable = false;
  if (Array.isArray(node.type)) {
    const types = node.type.filter((t) => t !== 'null');
    nullable = types.length !== node.type.length;
    if (types.length !== 1) die(`multi-type schema at ${ctx}: ${JSON.stringify(js.type)}`);
    node.type = types[0];
  }
  // anyOf/oneOf with a null branch
  let variants = node.anyOf || node.oneOf;
  if (variants && variants.some((v) => v && v.type === 'null')) {
    variants = variants.filter((v) => !(v && v.type === 'null'));
    nullable = true;
    if (variants.length === 1) {
      const inner = jsonSchemaToIR(variants[0], defs, ctx, refStack);
      if (nullable) inner['x-ct-nullable'] = true;
      return inner;
    }
    node = { ...node, anyOf: variants, oneOf: undefined };
  }

  const ir = coreToIR(node, defs, ctx, refStack);
  if (nullable) ir['x-ct-nullable'] = true;
  return ir;
}

function coreToIR(node, defs, ctx, refStack) {
  const variants = node.anyOf || node.oneOf;
  if (variants) return unionToIR(variants, defs, ctx, refStack);

  if (node.const !== undefined) {
    if (typeof node.const !== 'string') die(`non-string const at ${ctx} — IR enums are string-valued`);
    return { type: 'string', 'x-ct-scalar': 'string', enum: [node.const] };
  }
  if (node.enum) {
    if (!node.enum.every((v) => typeof v === 'string')) {
      die(`non-string enum at ${ctx} — IR enums are string-valued (v1)`);
    }
    return { type: 'string', 'x-ct-scalar': 'string', enum: [...node.enum].sort() };
  }

  switch (node.type) {
    case 'string': {
      const scalar = FORMAT_SCALARS[node.format] || 'string';
      return { type: 'string', 'x-ct-scalar': scalar };
    }
    case 'number':
    case 'integer':
      // JS numbers are float64 regardless of integer constraints — mapping
      // z.int() to int64 would silence real >2^53 precision risk (SPEC F7).
      return { type: 'number', 'x-ct-scalar': 'float64' };
    case 'boolean':
      return { type: 'boolean', 'x-ct-scalar': 'bool' };
    case 'array': {
      if (node.prefixItems) die(`tuple at ${ctx} — not representable in IR v1`);
      return { type: 'array', items: jsonSchemaToIR(node.items, defs, ctx + '[]', refStack) };
    }
    case 'object': {
      const props = node.properties || {};
      const names = Object.keys(props);
      const openValues = node.additionalProperties && node.additionalProperties !== false;
      if (openValues && names.length === 0) {
        return { type: 'object', additionalProperties: true }; // z.record
      }
      if (openValues) die(`mixed record and named properties at ${ctx} — not representable`);
      if (names.length === 0) die(`object with no properties at ${ctx}`);
      const properties = {};
      for (const name of names) {
        properties[name] = jsonSchemaToIR(props[name], defs, ctx + '.' + name, refStack);
      }
      const out = { type: 'object', properties };
      const required = (node.required || []).filter((r) => names.includes(r)).sort();
      if (required.length > 0) out.required = required;
      return out;
    }
    default:
      die(`unsupported JSON Schema at ${ctx}: ${JSON.stringify(node).slice(0, 200)}`);
  }
}

function unionToIR(variants, defs, ctx, refStack) {
  const resolved = variants.map((v) => jsonSchemaToIR(v, defs, ctx, refStack));
  // All single-value string enums → flatten to one enum (z.literal unions).
  if (resolved.every((r) => r.enum && !r.properties)) {
    const values = [...new Set(resolved.flatMap((r) => r.enum))].sort();
    return { type: 'string', 'x-ct-scalar': 'string', enum: values };
  }
  if (!resolved.every((r) => r.properties)) {
    die(`unsupported union at ${ctx} — only object unions with a literal discriminant or string-literal unions`);
  }
  // Discriminated object union: shared property that is a single-value enum.
  let discriminator = null;
  const names = Object.keys(resolved[0].properties).sort();
  for (const name of names) {
    const vals = resolved.map((r) => {
      const p = r.properties[name];
      return p && p.enum && p.enum.length === 1 ? p.enum[0] : null;
    });
    if (vals.every((v) => v !== null) && new Set(vals).size === resolved.length) {
      discriminator = name;
      break;
    }
  }
  if (!discriminator) {
    die(`untagged object union at ${ctx} — add a shared literal discriminant (IR v1 supports tagged unions only)`);
  }
  const branches = resolved.map((r) => {
    const tag = r.properties[discriminator].enum[0];
    const branch = { ...r, properties: { ...r.properties } };
    delete branch.properties[discriminator];
    if (branch.required) {
      branch.required = branch.required.filter((x) => x !== discriminator);
      if (branch.required.length === 0) delete branch.required;
    }
    branch['x-ct-discriminator-value'] = tag;
    return branch;
  });
  branches.sort((a, b) => (a['x-ct-discriminator-value'] < b['x-ct-discriminator-value'] ? -1 : 1));
  return { 'x-ct-discriminator': discriminator, oneOf: branches };
}

// === path 1: TypeScript types (compiler API) ================================

function isDateType(t) {
  // Robust across TS lib reorganizations (5.x hasNoDefaultLib, 6.x split libs):
  // the global Date is any symbol named Date declared in a default lib.*.d.ts.
  return !!(t.symbol && t.symbol.name === 'Date' &&
    t.symbol.declarations && t.symbol.declarations.some((d) => {
      const sf = d.getSourceFile();
      return sf.isDeclarationFile && /(^|[\\/])lib(\.[^\\/]+)?\.d\.[cm]?ts$/.test(sf.fileName);
    }));
}

function scalarNode(scalar) {
  const jsonType = { bool: 'boolean', int32: 'integer', int64: 'integer',
    float32: 'number', float64: 'number', decimal: 'number' }[scalar] || 'string';
  return { type: jsonType, 'x-ct-scalar': scalar };
}

function splitNullable(t) {
  if (!(t.flags & ts.TypeFlags.Union)) return { nullable: false, members: [t] };
  let nullable = false;
  const members = [];
  for (const m of t.types) {
    if (m.flags & ts.TypeFlags.Null) { nullable = true; continue; }
    if (m.flags & ts.TypeFlags.Undefined) continue; // optionality, handled at the property
    members.push(m);
  }
  return { nullable, members };
}

function schemaFor(rawType, stack, ctx) {
  const { nullable, members } = splitNullable(rawType);
  const node = schemaForMembers(members, stack, ctx);
  if (nullable) node['x-ct-nullable'] = true;
  return node;
}

function schemaForMembers(members, stack, ctx) {
  if (members.length === 0) die(`only null/undefined remain at ${ctx}`);
  if (members.length === 1) return schemaForSingle(members[0], stack, ctx);

  if (members.every((m) => m.flags & ts.TypeFlags.BooleanLiteral)) return scalarNode('bool');

  if (members.every((m) => m.flags & ts.TypeFlags.StringLiteral)) {
    return { type: 'string', 'x-ct-scalar': 'string', enum: members.map((m) => m.value).sort() };
  }

  if (members.every((m) => m.flags & ts.TypeFlags.Object)) {
    return tsUnionFor(members, stack, ctx);
  }
  die(`unsupported union at ${ctx}: ` + members.map((m) => checker.typeToString(m)).join(' | '));
}

function schemaForSingle(t, stack, ctx) {
  if (t.flags & ts.TypeFlags.String) return scalarNode('string');
  if (t.flags & ts.TypeFlags.Number) return scalarNode('float64');
  if (t.flags & ts.TypeFlags.BigInt) return scalarNode('int64');
  if (t.flags & (ts.TypeFlags.Boolean | ts.TypeFlags.BooleanLiteral)) return scalarNode('bool');
  if (t.flags & ts.TypeFlags.StringLiteral) {
    return { type: 'string', 'x-ct-scalar': 'string', enum: [t.value] };
  }
  if (t.flags & (ts.TypeFlags.NumberLiteral | ts.TypeFlags.BigIntLiteral)) {
    die(`numeric literal type at ${ctx} — wirefit enums are string-valued (IR v1)`);
  }
  if (t.flags & (ts.TypeFlags.Any | ts.TypeFlags.Unknown)) {
    die(`untyped value (any/unknown) at ${ctx} — give it a concrete type or exclude it`);
  }
  if (t.flags & ts.TypeFlags.EnumLike) {
    const members = t.flags & ts.TypeFlags.Union ? t.types : [t];
    if (!members.every((m) => m.flags & ts.TypeFlags.StringLiteral)) {
      die(`numeric TS enum at ${ctx} — only string enums are supported (IR v1)`);
    }
    return { type: 'string', 'x-ct-scalar': 'string', enum: members.map((m) => m.value).sort() };
  }
  if (t.flags & ts.TypeFlags.Union) return schemaFor(t, stack, ctx);

  if (!(t.flags & ts.TypeFlags.Object)) {
    die(`unsupported type "${checker.typeToString(t)}" at ${ctx}`);
  }

  if (isDateType(t)) return scalarNode('datetime');

  if (checker.isTupleType(t)) die(`tuple type at ${ctx} — not representable in IR v1`);

  if (checker.isArrayType(t)) {
    const elem = checker.getTypeArguments(t)[0];
    return { type: 'array', items: schemaFor(elem, stack, ctx + '[]') };
  }

  if (stack.includes(t)) return { 'x-ct-recursive': true };

  const props = checker.getPropertiesOfType(t);
  const stringIndex = checker.getIndexInfoOfType(t, ts.IndexKind.String);
  if (stringIndex) {
    if (props.length > 0) die(`mixed index signature and named properties at ${ctx} — not representable`);
    return { type: 'object', additionalProperties: true };
  }
  if (props.length === 0) {
    if (t.getCallSignatures().length > 0) die(`function type at ${ctx}`);
    die(`object with no properties at ${ctx} (${checker.typeToString(t)})`);
  }

  stack.push(t);
  try {
    const properties = {};
    const required = [];
    for (const prop of props) {
      const name = prop.getName();
      const declNode = (prop.declarations && prop.declarations[0]) || prop.valueDeclaration;
      const pt = checker.getTypeOfSymbolAtLocation(prop, declNode);
      const optional = !!(prop.flags & ts.SymbolFlags.Optional);
      if (prop.flags & ts.SymbolFlags.Method || pt.getCallSignatures().length > 0) {
        die(`method/function property "${name}" at ${ctx} — DTOs must be plain data`);
      }
      properties[name] = schemaFor(pt, stack, ctx + '.' + name);
      if (!optional) required.push(name);
    }
    const node = { type: 'object', properties };
    if (required.length > 0) node.required = required.sort();
    return node;
  } finally {
    stack.pop();
  }
}

function tsUnionFor(members, stack, ctx) {
  const candidates = new Map();
  for (const m of members) {
    for (const prop of checker.getPropertiesOfType(m)) {
      const declNode = (prop.declarations && prop.declarations[0]) || prop.valueDeclaration;
      const pt = checker.getTypeOfSymbolAtLocation(prop, declNode);
      if (pt.flags & ts.TypeFlags.StringLiteral) {
        if (!candidates.has(prop.getName())) candidates.set(prop.getName(), []);
        candidates.get(prop.getName()).push(pt.value);
      }
    }
  }
  let discriminator = null;
  for (const [name, values] of [...candidates.entries()].sort()) {
    if (values.length === members.length && new Set(values).size === members.length) {
      discriminator = name;
      break;
    }
  }
  if (!discriminator) {
    die(`untagged object union at ${ctx} — add a shared literal discriminant property (IR v1 supports tagged unions only)`);
  }
  const branches = [];
  for (const m of members) {
    const dProp = checker.getPropertiesOfType(m).find((p) => p.getName() === discriminator);
    const dt = checker.getTypeOfSymbolAtLocation(dProp, dProp.declarations[0]);
    const tag = dt.value;
    const branch = schemaForSingle(m, stack, ctx + '<' + tag + '>');
    if (branch.properties) {
      delete branch.properties[discriminator];
      if (branch.required) {
        branch.required = branch.required.filter((r) => r !== discriminator);
        if (branch.required.length === 0) delete branch.required;
      }
    }
    branch['x-ct-discriminator-value'] = tag;
    branches.push(branch);
  }
  branches.sort((a, b) => (a['x-ct-discriminator-value'] < b['x-ct-discriminator-value'] ? -1 : 1));
  return { 'x-ct-discriminator': discriminator, oneOf: branches };
}
