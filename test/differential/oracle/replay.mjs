// SPDX-License-Identifier: Apache-2.0
import { Context, Service } from '@deepseek-ai/cordis';
import { readFileSync, writeFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { isDeepStrictEqual } from 'node:util';

const undef = { $undefined: true };
const states = ['PENDING', 'LOADING', 'ACTIVE', 'FAILED', 'DISPOSED', 'UNLOADING'];
const isPromise = value => value && typeof value.then === 'function';
const hash = value => createHash('sha256').update(value).digest('hex');
const err = error => ({ name: error?.name ?? 'Error', message: String(error?.message ?? error), ...(error?.code ? { code: error.code } : {}), ...(error instanceof AggregateError ? { errors: error.errors.map(err) } : {}) });

export function createReplay(script, { causalWitness = false } = {}) {
  if (script.schema !== 's0-script/v1' || !/^D\d\d$/.test(script.id)) throw new Error('invalid script identity');
  const trace = { schema: 's0-trace/v1', scenario: script.id, events: [], checkpoints: [] };
  // Witness cuts are observations of the raw trace, never additional trace data.
  const checkpointAfterEvent = causalWitness ? [] : undefined;
  const root = new Context();
  const contexts = new Map([['root', root]]), fibers = new Map(), plugins = new Map(), fiberIDs = new WeakMap();
  const labels = new Map(), realmIDs = new Map(), outputs = {}, gates = new Map(), marks = new Map(), waiters = new Map(), tasks = new Map(), serviceValues = new WeakMap(), disposers = new Map(), appliedConfigs = new WeakMap();
  let constructing;
  const event = (kind, data) => trace.events.push({ seq: trace.events.length + 1, kind, data: snapshot(data) });
  function snapshot(value, seen = new Set()) {
    if (value === undefined) return { ...undef };
    if (value === null || typeof value !== 'object') return value;
    if (value instanceof Error) return err(value);
    if (serviceValues.has(value)) return snapshot(serviceValues.get(value), seen);
    // Tracked service facades inherit the original value reference.
    if (value instanceof Service && value.value !== undefined) return snapshot(value.value, seen);
    if (seen.has(value)) throw new Error('cyclic fixture value');
    seen.add(value);
    const out = Array.isArray(value) ? value.map(item => snapshot(item, seen)) : Object.fromEntries(Object.entries(value).map(([k, v]) => [k, snapshot(v, seen)]));
    seen.delete(value);
    return out;
  }
  const fid = fiber => !fiber ? '<unmapped>' : fiberIDs.get(fiber) ?? fiberIDs.get(Object.getPrototypeOf(fiber)) ?? (fiber === root.fiber ? 'root' : '<unmapped>');
  root.on('internal/plugin', fiber => {
    if (fiber.uid !== null && !fiberIDs.has(fiber)) {
      if (!constructing) throw new Error('unmapped plugin publication');
      fiberIDs.set(fiber, constructing);
      fibers.set(constructing, fiber);
      contexts.set(constructing, fiber.ctx);
    }
    event('plugin', { fiber: fid(fiber), disposed: fiber.uid === null });
  });
  root.on('internal/status', (fiber, old) => event('status', { fiber: fid(fiber), from: states[old], to: states[fiber.state] }));
  root.on('internal/service', function (key, value) { event('service', { key, realm: realmIDs.get(this?.[Context.isolate]?.[key]) ?? `root:${key}`, value }); }, { global: true });
  root.on('internal/commit', (fiber, config) => event('commit', { fiber: fid(fiber), config }));
  root.on('internal/config', (fiber, next) => {
    const value = next();
    event('config', { fiber: fid(fiber), config: value });
    return value;
  });
  root.logger.exporter({ export(message) { if (message.type === 'error') event('error', { fiber: fid(message.fiber?.deref()), error: err(message.args[0]) }); } });

  function extendContext(id, parent, spec) {
    if (!parent || contexts.has(id)) throw new Error(`invalid context ${id}`);
    let ctx = spec.meta ? parent.extend(spec.meta) : parent.extend();
    if (spec.isolate) {
      const { key, label } = spec.isolate;
      const name = label ? `${key}:${label}` : `${key}:@${id}`;
      if (!labels.has(name)) labels.set(name, Symbol(name));
      const symbol = labels.get(name);
      realmIDs.set(symbol, name);
      ctx = ctx.isolate(key, symbol);
    }
    if (spec.intercept) ctx = ctx.intercept(spec.intercept.key, spec.intercept.config);
    contexts.set(id, ctx);
  }
  for (const def of script.contexts ?? []) extendContext(def.id, contexts.get(def.parent), def);
  function evaluate(value, env) {
    if (value === undefined || value === null || typeof value !== 'object') return value;
    const path = (object, p) => p === '' ? object : String(p).split('.').reduce((o, k) => o?.[k], object);
    if (Object.hasOwn(value, '$config')) return path(env.config, value.$config);
    if (Object.hasOwn(value, '$meta')) return path(env.ctx, value.$meta);
    if (Object.hasOwn(value, '$output')) return path(outputs, value.$output);
    if (Object.hasOwn(value, '$undefined')) return undefined;
    return Array.isArray(value) ? value.map(v => evaluate(v, env)) : Object.fromEntries(Object.entries(value).map(([k, v]) => [k, evaluate(v, env)]));
  }
  const put = (name, value) => { if (name) { outputs[name] = snapshot(value); event('output', { to: name, value }); } };
  const envFor = (ctx = root, config, id = 'root') => ({ ctx, config, id });
  function mark(label, kind, value) {
    event(kind, { label, value });
    marks.set(label, (marks.get(label) ?? 0) + 1);
    for (const waiter of waiters.get(label) ?? []) if (marks.get(label) >= waiter.count) waiter.resolve();
  }
  function gate(label) {
    let value = gates.get(label);
    if (!value) {
      let resolve;
      const promise = new Promise(done => { resolve = done; });
      value = { promise, resolve };
      gates.set(label, value);
    }
    return value;
  }
  function track(value, name) {
    if (!isPromise(value)) return value;
    const promise = Promise.resolve(value);
    // Settled rejected fixture tasks remain observable without unhandled rejection.
    promise.catch(() => {});
    tasks.set(name ?? `task:${tasks.size + 1}`, promise);
    return promise;
  }
  function run(program, env) {
    let index = 0, result;
    const advance = () => {
      while (index < program.length) {
        const action = program[index++];
        const returned = execute(action, env);
        if (isPromise(returned)) return returned.then(value => { result = value; return advance(); });
        if (action.op === 'return') return returned;
        result = returned;
      }
      return result;
    };
    return advance();
  }
  function execute(action, enclosing) {
    const contextName = action.context ?? (['mount', 'child'].includes(action.op) ? undefined : action.fiber);
    const ctx = contextName ? contexts.get(contextName) : enclosing.ctx;
    if (!ctx) throw new Error(`unknown context ${contextName}`);
    const env = { ...enclosing, ctx };
    const value = evaluate(action.value, env);
    const label = evaluate(action.label, env);
    const target = () => { const f = fibers.get(action.fiber); if (!f) throw new Error(`unknown fiber ${action.fiber}`); return f; };
    const attempt = () => {
      switch (action.op) {
        case 'context': {
          const spec = { meta: evaluate(action.meta, env), isolate: action.mode === 'isolate' ? { key: action.key, label: action.label } : undefined,
            intercept: action.mode === 'intercept' ? { key: action.key, config: evaluate(action.config, env) } : undefined };
          extendContext(action.to, ctx, spec); return;
        }
        case 'when': {
          if (!isDeepStrictEqual(evaluate(action.base, env), value)) return;
          return run(action.program ?? [], env);
        }
        case 'mount': case 'child': {
          const plugin = plugins.get(action.plugin);
          if (!plugin || fibers.has(action.fiber)) throw new Error('invalid mount');
          const previous = constructing;
          constructing = action.fiber;
          try { ctx.plugin(plugin, evaluate(action.config, env)); } finally { constructing = previous; }
          return;
        }
        case 'settle': return settle().then(() => checkpoint(label));
        case 'snapshot': return checkpoint(label);
        case 'waitMark': {
          const count = action.count || 1;
          if ((marks.get(label) ?? 0) >= count) return;
          return new Promise(resolve => {
            const list = waiters.get(label) ?? [];
            list.push({ count, resolve }); waiters.set(label, list);
          });
        }
        case 'mark': mark(label, 'mark', value); return;
        case 'gate': mark(label, 'gate', value); return gate(label).promise;
        case 'release': gate(label).resolve(); return;
        case 'provide': {
          if (action.mode === 'service' || action.mode === 'available') {
            class FixtureService extends Service {
              constructor() { super(ctx, action.key); this.value = value; }
              [Service.check]() {
                if (action.mode !== 'available') return true;
                return !!outputs[value.ready]?.[this.ctx.audience];
              }
            }
            const service = new FixtureService(); serviceValues.set(service, value);
          } else {
            const disposer = ctx.provide(action.key, value);
            if (action.to) disposers.set(action.to, disposer);
          }
          return;
        }
        case 'notify': ctx.reflect.notify(action.keys); return;
        case 'set': ctx.set(action.key, value); return;
        case 'output': put(action.to, value); return;
        case 'read': {
          const result = action.mode === 'get' ? ctx.get(action.key, action.strict ?? true)
            : action.mode === 'config' ? ctx[action.key][Service.resolveConfig](evaluate(action.base, env), evaluate(action.head, env))
              : ctx[action.key];
          put(action.to, result); return;
        }
        case 'update': case 'restart': case 'dispose': {
          const fiber = target();
          const result = action.op === 'update' ? fiber.update(evaluate(action.config, env), action.mode === 'noSave') : fiber[action.op]();
          const promise = track(result, action.task);
          return action.await === false ? undefined : promise;
        }
        case 'withdraw': {
          const disposer = disposers.get(action.task);
          if (!disposer) throw new Error(`unknown disposer ${action.task}`);
          const promise = track(disposer(), action.to);
          return action.await === false ? undefined : promise;
        }
        case 'awaitFiber': {
          const promise = track(target().await(), action.task);
          return action.await === false ? undefined : promise;
        }
        case 'inspect': {
          const fiber = target();
          if (action.mode === 'effects') put(action.to, fiber.getEffects().map(effect => effect.label));
          else if (action.mode === 'configIdentity') put(action.to, fiber.effectiveConfig === appliedConfigs.get(fiber));
          else throw new Error('unknown inspect mode');
          return;
        }
        case 'rawConfig': target().rawConfig = evaluate(action.config, env); return;
        case 'resolveConfig': put(action.to, target().resolveConfig(evaluate(action.config, env))); return;
        case 'selfDispose': {
          event('selfDispose', { fiber: enclosing.id, await: action.await === true });
          const promise = track(ctx.fiber.dispose(), action.task);
          return action.await === true ? promise : undefined;
        }
        case 'effect': {
          if (action.mode === 'partial') {
            ctx.effect(function* () {
              for (const cleanup of action.cleanup ?? []) yield () => run([cleanup], env);
              throw new Error(action.error ?? 'partial-effect-failed');
            }, label);
            return;
          }
          const disposer = ctx.effect(() => {
            const result = run(action.program ?? [], env);
            const cleanup = () => run(action.cleanup ?? [], env);
            return isPromise(result) ? result.then(() => cleanup) : cleanup;
          }, label);
          if (action.to) disposers.set(action.to, disposer);
          // Await setup, not effect ownership teardown. An explicit false keeps
          // setup running so owner teardown can join a late disposer.
          const setup = isPromise(disposer) ? track(Promise.resolve(disposer).then(() => undefined), action.task) : undefined;
          return action.await === false ? undefined : setup;
        }
        case 'lateEffect': target().ctx.effect(() => () => {}, label); return;
        case 'fail': throw new Error(action.error ?? 'fixture-failed');
        case 'listen': {
          const key = action.symbol ? Symbol.for(`s0:${action.key}`) : action.key;
          ctx.on(key, (...args) => {
            event('listener', { label });
            return run(action.program ?? [], { ...env, config: action.key === 'internal/update' ? args[0] : env.config, next: args.at(-1) });
          }, { prepend: action.prepend, global: action.global });
          return;
        }
        case 'dispatch': {
          const key = action.symbol ? Symbol.for(`s0:${action.key}`) : action.key;
          const args = action.mode === 'waterfall' ? [key, () => { event('inner', { key: action.key }); return value; }] : [key];
          const result = ctx[action.mode](...args);
          if (isPromise(result)) {
            const promise = track(Promise.resolve(result).then(v => put(action.to, v)), action.task);
            return action.await === false ? undefined : promise;
          }
          put(action.to, result); return;
        }
        case 'return': return value;
        case 'next': {
          if (typeof env.next !== 'function') throw new Error('next outside waterfall');
          const result = env.next();
          if (isPromise(result)) return result.then(v => { put(action.to, v); return v; });
          put(action.to, result); return result;
        }
        case 'awaitTask': { const task = tasks.get(action.task); if (!task) throw new Error(`unknown task ${action.task}`); return task; }
        default: throw new Error(`unknown action ${action.op}`);
      }
    };
    try {
      const result = attempt();
      if (action.catch && isPromise(result)) return result.catch(error => { event('error', { op: action.op, error: err(error) }); put(action.to, err(error)); });
      return result;
    } catch (error) {
      if (!action.catch) throw error;
      event('error', { op: action.op, error: err(error) }); put(action.to, err(error));
    }
  }
  for (const spec of script.plugins ?? []) {
    if (plugins.has(spec.id)) throw new Error('duplicate plugin ID');
    const apply = (ctx, config) => {
      const id = fid(ctx.fiber);
      appliedConfigs.set(ctx.fiber, config);
      const env = envFor(ctx, config, id);
      event('apply', { fiber: id, config });
      if (spec.update) ctx.on('internal/update', (candidate, next) => {
        event('update', { fiber: id, config: candidate, mode: spec.update });
        if (spec.update === 'retain') return;
        if (spec.update === 'unchanged' && isDeepStrictEqual(ctx.fiber.effectiveConfig, candidate)) return;
        if (spec.update === 'reject') throw new Error('fixture-update-rejected');
        return next();
      });
      const result = run(spec.apply, env);
      const cleanup = spec.cleanup?.length ? () => { event('cleanup', { fiber: id }); return run(spec.cleanup, env); } : undefined;
      return isPromise(result) ? result.then(() => cleanup) : cleanup;
    };
    const plugin = { name: spec.id, inject: spec.needs, apply };
    plugins.set(spec.id, plugin);
    if (spec.configTransform) root.on('internal/config', (fiber, next) => {
      const value = next();
      return fiber.runtime?.callback === apply ? { ...value, ...spec.configTransform } : value;
    }, { prepend: true });
  }
  async function settle() {
    for (let pass = 0; pass < 1000; pass++) {
      const pending = [...fibers.values()].map(f => f.inertia).filter(Boolean);
      // Fiber.await() can legitimately settle PENDING or reject FAILED. Inertia
      // only is the stable causal work barrier; errors remain in state/log trace.
      if (pending.length) await Promise.all(pending);
      const taskBatch = [...tasks]; tasks.clear();
      if (taskBatch.length) await Promise.allSettled(taskBatch.map(([, p]) => p));
      if (![...fibers.values()].some(f => f.inertia) && !tasks.size) return;
    }
    throw new Error('settle did not converge');
  }
  function checkpoint(label) {
    if (!label || trace.checkpoints.some(c => c.label === label)) throw new Error('invalid checkpoint label');
    const snapshots = [...fibers].map(([id, f]) => {
      const epoch = f._runner.epoch;
      return { fiber: id, state: states[f.state], fiberUID: f.uid, revision: f._revision,
        epoch: epoch === '__INACTIVE__' ? null : { revision: Number(epoch.split(':')[0]), needs: Object.keys(f.inject).map((key, i) => ({ key, provisionUID: Number(epoch.split(':')[i + 1]) })) },
        rawConfig: snapshot(f.rawConfig), effectiveConfig: snapshot(f.effectiveConfig) };
    });
    const provisions = Reflect.ownKeys(root.reflect.store).map(key => {
      const p = root.reflect.store[key];
      return { key: p.name, realm: realmIDs.get(p.isolate) ?? `root:${p.name}`, provider: fid(p.ctx.fiber), provisionUID: p.uid, value: snapshot(p.value), active: p.ctx.fiber.state === 2 };
    });
    trace.checkpoints.push({ label, states: snapshots, provisions, outputs: snapshot(outputs) });
    // Capture only after the entire synchronous checkpoint has been cloned.
    checkpointAfterEvent?.push({ label, afterEvent: trace.events.length });
  }
  const finished = run(script.steps, envFor());
  return { trace, done: Promise.resolve(finished), checkpoint, ...(causalWitness ? { checkpointAfterEvent } : {}) };
}

function assertExpectations(script, trace) {
  for (const assertion of script.expect ?? []) {
    const checkpoint = trace.checkpoints.find(item => item.label === assertion.at);
    if (!checkpoint) throw new Error(`missing checkpoint: ${assertion.at}`);
    const actual = assertion.path === '' ? checkpoint : assertion.path.slice(1).split('/').reduce((v, key) => v?.[key.replaceAll('~1', '/').replaceAll('~0', '~')], checkpoint);
    if (!isDeepStrictEqual(actual, assertion.equals)) throw new Error(`${script.id} assertion ${assertion.at}${assertion.path}: ${JSON.stringify(actual)} != ${JSON.stringify(assertion.equals)}`);
  }
}

async function main() {
  const scriptPath = process.argv[2], outputPath = process.argv[3];
  if (!scriptPath) throw new Error('usage: node --import ./register.mjs ./replay.mjs SCRIPT.json [TRACE.json]');
  const bytes = readFileSync(scriptPath), script = JSON.parse(bytes);
  const startedUTC = new Date().toISOString();
  const witnessPath = process.env.S0_CAUSAL_WITNESS_PATH;
  const replay = createReplay(script, { causalWitness: !!witnessPath });
  let timer;
  if (script.diagnostic === 'self-dispose-await') {
    const boundMS = 200;
    const diagnostic = new Promise(resolve => { timer = setTimeout(() => {
      replay.checkpoint('diagnostic');
      replay.trace.diagnostic = { code: 'SELF_DISPOSE_AWAIT_BLOCKED', boundMS, message: 'Apply awaited its own disposal; the load/dispose cycle did not settle before the diagnostic bound.' };
      resolve();
    }, boundMS); });
    await Promise.race([replay.done, diagnostic]);
    clearTimeout(timer);
  } else {
    const guard = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('S0_ORACLE_TIMEOUT')), 5000); });
    try { await Promise.race([replay.done, guard]); } finally { clearTimeout(timer); }
  }
  let assertionError;
  try { assertExpectations(script, replay.trace); } catch (error) { assertionError = error; }
  if (globalThis.__s0Resolver.nativeAttempts) throw new Error('native loader attempted');
  const text = JSON.stringify(replay.trace, null, 2) + '\n';
  if (outputPath) {
    writeFileSync(outputPath, text, { flag: 'wx' });
    const record = { schema: 's0-oracle-run/v1', resolverSHA256: hash(readFileSync(new URL('./register.mjs', import.meta.url))), baselineCommit: '2d8d6ec717bb327739d1630d03ad6787c37660d6', command: ['S0_NODE', '--import', 'oracle/register.mjs', 'oracle/replay.mjs', `corpus/${script.id}.json`, 'OUTPUT'], result: assertionError ? 'fail' : script.diagnostic ? 'diagnostic' : 'pass', ...(assertionError ? { assertionError: err(assertionError) } : {}), criteriaSHA256: '628bd6ec24e5983bfbe9fcdada096eaa84db3e95227f11e8fd5a7fa33a26f19c', sourceCommit: 'fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159', startedUTC, endedUTC: new Date().toISOString(), nodeVersion: process.version, executableSHA256: hash(readFileSync(process.execPath)), oracleSHA256: hash(readFileSync(fileURLToPath(import.meta.url))), vendorManifestSHA256: hash(readFileSync(new URL('./manifest.json', import.meta.url))), scripts: { [script.id]: hash(bytes) }, exitCode: assertionError ? 1 : 0, nativeAttempts: globalThis.__s0Resolver.nativeAttempts, resolvedPackages: globalThis.__s0Resolver.resolved, traceSHA256: hash(text) };
    writeFileSync(outputPath + '.run.json', JSON.stringify(record, null, 2) + '\n', { flag: 'wx' });
  } else process.stdout.write(text);
  if (witnessPath && !assertionError && !script.diagnostic) {
    const witness = { schema: 's0-causal-witness/v1', traceSHA256: hash(text), scriptSHA256: hash(bytes), checkpoints: replay.checkpointAfterEvent };
    writeFileSync(witnessPath, JSON.stringify(witness, null, 2) + '\n', { flag: 'wx' });
  }
  if (assertionError) throw assertionError;
}
if (process.argv[1] === fileURLToPath(import.meta.url)) main().catch(error => { process.stderr.write(JSON.stringify(err(error)) + '\n'); process.exitCode = 1; });
