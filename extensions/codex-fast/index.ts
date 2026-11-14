import { readFileSync, renameSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { getAgentDir, type ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { registerCapability } from './capabilities.ts';
import { CODEX_FAST_CAPABILITY, CODEX_FAST_CHANGED, InvalidFastLevelError, type CodexFastService, type FastConfig, type FastLevel } from './api.ts';

const levels: readonly FastLevel[] = Object.freeze(['normal', 'fast', 'ultrafast']);

const configPath = join(getAgentDir(), 'codex-fast.json');

const loadConfig = (fallback: FastConfig = { level: 'normal' }): FastConfig => {
    try {
        const config = JSON.parse(readFileSync(configPath, 'utf8'));
        if (!config || typeof config !== 'object' || Array.isArray(config)) return { level: 'normal' };
        if (Object.hasOwn(config, 'level')) {
            return { level: levels.includes(config.level) ? config.level : 'normal' };
        }
        return { level: config.ultrafast === true ? 'ultrafast' : config.fast === true ? 'fast' : 'normal' };
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
            console.error(`读取 Fast 配置失败: ${error}`);
            return fallback;
        }
        return { level: 'normal' };
    }
};

const saveConfig = (config: FastConfig): void => {
    const tempPath = `${configPath}.${process.pid}.tmp`;
    writeFileSync(tempPath, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
    renameSync(tempPath, configPath);
};

export default function (pi: ExtensionAPI) {
    // The shared file supplies startup defaults; live settings belong to this instance.
    let config = loadConfig();

    const service: CodexFastService = {
        levels,
        getConfig() {
            return { ...config };
        },
        setLevel(level) {
            if (typeof level !== 'string' || !levels.includes(level as FastLevel)) throw new InvalidFastLevelError(level);
            const nextConfig: FastConfig = { level: level as FastLevel };
            saveConfig(nextConfig);
            config = nextConfig;
            // Observers cannot turn a successful commit into a reported failure.
            try { pi.events.emit(CODEX_FAST_CHANGED, Object.freeze({ ...config })); } catch {}
            return { ...config };
        },
    };
    registerCapability(pi, CODEX_FAST_CAPABILITY, service);

    pi.on('before_provider_request', (event, ctx) => {
        const model = ctx.model;
        if (
            model?.provider !== 'openai-codex' ||
            model.api !== 'openai-codex-responses' ||
            !event.payload ||
            typeof event.payload !== 'object' ||
            Array.isArray(event.payload)
        ) {
            return;
        }

        const serviceTier = config.level === 'ultrafast' && model.id === 'gpt-6-astra'
            ? 'ultrafast'
            : config.level !== 'normal' ? 'priority' : undefined;

        if (!serviceTier) {
            return;
        }

        return {
            ...(event.payload as Record<string, unknown>),
            service_tier: serviceTier,
        };
    });
}
