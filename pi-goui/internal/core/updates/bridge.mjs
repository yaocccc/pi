// Read-only native management APIs; no SDK, extensions, auth or model runtime.
import { readFileSync, existsSync, statSync } from 'node:fs';
import { basename, join } from 'node:path';
import { pathToFileURL } from 'node:url';

const input = JSON.parse(readFileSync(0, 'utf8'));
const report = { currentVersion: input.version, packages: [] };
const emit = value => process.stdout.write(JSON.stringify(value));
try {
  const moduleURL = name => pathToFileURL(join(input.root, 'dist', name)).href;
  let DefaultPackageManager, checkForNewPiVersion;
  try {
    ({ DefaultPackageManager } = await import(moduleURL('core/package-manager.js')));
    ({ checkForNewPiVersion } = await import(moduleURL('utils/version-check.js')));
    if (typeof DefaultPackageManager !== 'function' || typeof checkForNewPiVersion !== 'function') throw 0;
  } catch {
    emit({ ...report, unsupported: 'This Pi installation has no compatible update-check API; update it manually.' });
    process.exit(0);
  }
  const settingsPath = join(input.agentDir, 'settings.json');
  let settings = {};
  if (existsSync(settingsPath)) {
    if (statSync(settingsPath).size > 1024 * 1024) throw 0;
    settings = JSON.parse(readFileSync(settingsPath, 'utf8').replace(/^\uFEFF/, ''));
  }
  if (!settings || typeof settings !== 'object' || (settings.packages !== undefined && !Array.isArray(settings.packages))) throw 0;
  // Do not automatically execute arbitrary custom package-manager wrappers.
  if (settings.npmCommand !== undefined && settings.npmCommand !== null) {
    const command = settings.npmCommand;
    if (!Array.isArray(command) || (command.length > 0 && (command.length !== 1 || typeof command[0] !== 'string' || !['npm', 'pnpm', 'bun'].includes(basename(command[0]).replace(/\.(cmd|exe)$/i, ''))))) {
      emit({ ...report, unsupported: 'Custom npm command wrappers require manual update checks.' });
      process.exit(0);
    }
  }
  // Avoid SettingsManager.create migrations/saves and exclude project settings.
  const manager = new DefaultPackageManager({
    cwd: input.agentDir, agentDir: input.agentDir,
    settingsManager: {
      getGlobalSettings: () => settings, getProjectSettings: () => ({}),
      getNpmCommand: () => settings.npmCommand ? [...settings.npmCommand] : undefined,
      isProjectTrusted: () => false,
    },
  });
  const [release, available] = await Promise.all([
    checkForNewPiVersion(input.version), manager.checkForAvailableUpdates(),
  ]);
  if (release && typeof release.version === 'string') report.latestVersion = release.version;
  const sources = new Set((settings.packages ?? []).map(p => typeof p === 'string' ? p : p?.source));
  const seen = new Set();
  for (const item of available) {
    if (item.scope !== 'user' || !sources.has(item.source) || seen.has(item.source)) continue;
    const parsed = manager.parseSource(item.source);
    if (!['npm', 'git'].includes(parsed.type) || parsed.pinned) continue;
    seen.add(item.source);
    report.packages.push({ source: item.source, type: parsed.type, scope: 'user' });
  }
  emit(report);
} catch {
  // Never expose module errors, registry credentials, package URLs or stderr.
  emit({ error: 'Update metadata unavailable.' });
}
