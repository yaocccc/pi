import { readFileSync, renameSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { getAgentDir, type ExtensionAPI } from '@earendil-works/pi-coding-agent';

interface FastConfig {
    fast: boolean;
    ultrafast: boolean;
}

const configPath = join(getAgentDir(), 'codex-fast.json');

const loadConfig = (): FastConfig => {
    try {
        const config = JSON.parse(readFileSync(configPath, 'utf8')) as Partial<FastConfig>;
        return { fast: config.fast === true, ultrafast: config.ultrafast === true };
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
            console.error(`读取 Fast 配置失败: ${error}`);
        }
        return { fast: false, ultrafast: false };
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
        description: '设置 Codex Fast / Ultrafast 模式',
        handler: async (_args, ctx) => {
            if (!ctx.hasUI) {
                ctx.ui.notify('Codex Fast 设置需要交互式 UI', 'warning');
                return;
            }

            while (true) {
                const items = [
                    { key: 'fast', name: 'Fast', label: `Fast · ${config.fast ? '开启' : '关闭'}` },
                    { key: 'ultrafast', name: 'Ultrafast', label: `Ultrafast · ${config.ultrafast ? '开启' : '关闭'}（仅 gpt-6-astra）` },
                ] as const;
                const selected = await ctx.ui.select(
                    'Codex Fast 设置 · 回车切换并保存 · Esc 退出',
                    items.map((item) => item.label),
                );
                if (selected === undefined) return;
                const item = items.find((item) => item.label === selected);
                if (!item) continue;
                const nextConfig = { ...config, [item.key]: !config[item.key] };

                try {
                    saveConfig(nextConfig);
                    config = nextConfig;
                    ctx.ui.notify(`${item.name} 模式已${config[item.key] ? '开启' : '关闭'}`, 'info');
                } catch (error) {
                    ctx.ui.notify(`保存 Fast 配置失败: ${error}`, 'error');
                }
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

        const serviceTier = config.ultrafast && model.id === 'gpt-6-astra'
            ? 'ultrafast'
            : config.fast ? 'priority' : undefined;

        if (!serviceTier) {
            return;
        }

        return {
            ...(event.payload as Record<string, unknown>),
            service_tier: serviceTier,
        };
    });
}
