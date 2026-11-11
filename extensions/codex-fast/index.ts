import { readFileSync, renameSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { getAgentDir, type ExtensionAPI } from '@earendil-works/pi-coding-agent';

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
                ctx.ui.notify('Codex Fast 设置需要交互式 UI', 'warning');
                return;
            }

            config = loadConfig(config);
            const items = levels.map((level) => ({
                level,
                label: `${level}${level === config.level ? ' · 当前' : ''}`,
            }));
            const selected = await ctx.ui.select(
                `Codex 档位 · 当前 ${config.level} · 回车选择并保存 · Esc 退出`,
                items.map((item) => item.label),
            );
            const item = items.find((item) => item.label === selected);
            if (!item) return;
            const nextConfig: FastConfig = { level: item.level };
            try {
                saveConfig(nextConfig);
                config = nextConfig;
                ctx.ui.notify(`Codex 档位已设为 ${config.level}`, 'info');
            } catch (error) {
                ctx.ui.notify(`保存 Fast 配置失败: ${error}`, 'error');
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
