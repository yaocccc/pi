import { readFileSync, renameSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { getAgentDir, type ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { emitFrontendEvent, isGoUI } from '../shared/frontend.ts';
import { notifyNativeError, notifyNativeSaved, notifyNativeUnavailable, selectNativeLevel } from './native-ui.ts';

type FastLevel = 'normal' | 'fast' | 'ultrafast';
interface FastConfig {
    level: FastLevel;
}
const levels: FastLevel[] = ['normal', 'fast', 'ultrafast'];

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
    let config = loadConfig();

    pi.registerCommand('codex-fast', {
        description: '设置 Codex 档位：normal / fast / ultrafast',
        handler: async (_args, ctx) => {
            if (!ctx.hasUI) {
                if (!isGoUI(ctx)) notifyNativeUnavailable(ctx);
                return;
            }

            config = loadConfig(config);
            const goUI = isGoUI(ctx);
            const selected = goUI
                ? await ctx.ui.input('pi-goui:codex-fast:v1', JSON.stringify({ level: config.level }))
                : await selectNativeLevel(ctx, config.level);
            if (selected === undefined) return;
            if (!levels.includes(selected as FastLevel)) {
                if (goUI) emitFrontendEvent(ctx, 'codex.settings-error', { code: 'invalid-level', message: selected });
                return;
            }
            const nextConfig: FastConfig = { level: selected as FastLevel };
            try {
                saveConfig(nextConfig);
                config = nextConfig;
                if (goUI) emitFrontendEvent(ctx, 'codex.settings-saved', { level: config.level });
                else notifyNativeSaved(ctx, config.level);
            } catch (error) {
                if (goUI) emitFrontendEvent(ctx, 'codex.settings-error', { message: error instanceof Error ? error.message : String(error) });
                else notifyNativeError(ctx, error);
            }
        },
    });

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

        // GUI and other sessions atomically replace the shared level file.
        // Reload for each eligible request; transient read failures retain the last level.
        config = loadConfig(config);
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
