// SPDX-License-Identifier: Apache-2.0
// This resolver changes resolution only, never vendored runtime bytes.
import { registerHooks } from 'node:module';
import { readFileSync, realpathSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';

if (process.versions.node !== '24.22.1') throw new Error('S0 requires isolated Node 24.22.1');
if (process.execArgv.some(arg => arg.includes('expose-internals'))) throw new Error('Node internals are forbidden');
const root = new URL('./', import.meta.url);
const manifest = JSON.parse(readFileSync(new URL('manifest.json', root), 'utf8'));
if (manifest.sourceCommit !== 'fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159') throw new Error('source pin mismatch');
for (const file of [...manifest.packages.flatMap(pkg => pkg.files), ...manifest.additionalNotices]) {
  const bytes = readFileSync(new URL(file.path, root));
  if (bytes.length !== file.bytes || createHash('sha256').update(bytes).digest('hex') !== file.sha256) {
    throw new Error(`vendored byte mismatch: ${file.path}`);
  }
}
const entries = new Map([
  ['@deepseek-ai/cordis', 'vendor/cordis/lib/index.js'],
  ['@deepseek-ai/cordis-plugin-loader', 'vendor/loader/lib/index.js'],
  ['@deepseek-ai/cosmokit', 'vendor/cosmokit/lib/index.js'],
  ['@deepseek-ai/schemastery', 'vendor/schemastery/lib/index.mjs'],
  ['@standard-schema/spec', 'vendor/standard-schema-spec/dist/index.js'],
]);
const allowedRoot = realpathSync(fileURLToPath(root)) + '/';
globalThis.__s0Resolver = { nativeAttempts: 0, resolved: [] };
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.includes('node-addon') || specifier.endsWith('.node') || specifier.startsWith('internal/')) {
      globalThis.__s0Resolver.nativeAttempts++;
      throw new Error(`S0_NATIVE_FORBIDDEN: ${specifier}`);
    }
    if (entries.has(specifier)) {
      const url = new URL(entries.get(specifier), root).href;
      globalThis.__s0Resolver.resolved.push(specifier);
      return { url, shortCircuit: true };
    }
    if (specifier.startsWith('node:')) return nextResolve(specifier, context);
    if (!specifier.startsWith('.') && !specifier.startsWith('/') && !specifier.startsWith('file:')) {
      throw new Error(`S0_UNPINNED_IMPORT: ${specifier}`);
    }
    const result = nextResolve(specifier, context);
    if (result.url.startsWith('file:') && !realpathSync(fileURLToPath(result.url)).startsWith(allowedRoot)) {
      throw new Error(`S0_OUTSIDE_ORACLE: ${specifier}`);
    }
    return result;
  },
  load(url, context, nextLoad) {
    if (url.endsWith('.node')) throw new Error('S0_NATIVE_FORBIDDEN');
    return nextLoad(url, context);
  },
});
