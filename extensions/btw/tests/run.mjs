import { execFileSync } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath, pathToFileURL } from 'node:url';

const here = fileURLToPath(new URL('.', import.meta.url));
const sdk = process.env.PI_CODING_AGENT_DIR || join(execFileSync('npm', ['root', '-g'], { encoding: 'utf8' }).trim(), '@earendil-works/pi-coding-agent');
const require = createRequire(join(sdk, 'package.json'));
const { build } = require('esbuild');
const temp = await mkdtemp(join(tmpdir(), 'pi-btw-test-'));
try {
  const output = join(temp, 'btw.mjs');
  await build({
    entryPoints: [resolve(here, '../index.ts')], outfile: output,
    bundle: true, platform: 'node', format: 'esm',
    plugins: [{ name: 'installed-pi', setup(build) {
      build.onResolve({ filter: /^@earendil-works\// }, ({ path }) => ({ path: path === '@earendil-works/pi-coding-agent' ? join(sdk, 'dist/index.js') : require.resolve(path), external: true }));
    } }],
  });
  process.env.BTW_MODULE = pathToFileURL(output).href;
  process.env.BTW_SDK = sdk;
  await import('./test.mjs');
} finally { await rm(temp, { recursive: true, force: true }); }
