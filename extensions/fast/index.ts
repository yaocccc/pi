import { readFileSync, renameSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { getAgentDir, type ExtensionAPI } from '@earendil-works/pi-coding-agent';

interface FastConfig {
    enabled: boolean;
}

const configPath = join(getAgentDir(), 'fast.json');

const loadConfig = (): FastConfig => {
    try {
        const config = JSON.parse(readFileSync(configPath, 'utf8')) as Partial<FastConfig>;
        return { enabled: config.enabled === true };
    } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') {
            console.error(`读取 Fast 配置失败: ${error}`);
        }
        return { enabled: false };
    }
};

const saveConfig = (config: FastConfig): void => {
    const tempPath = `${configPath}.${process.pid}.tmp`;
    writeFileSync(tempPath, `${JSON.stringify(config, null, 2)}\n`, { mode: 0o600 });
    renameSync(tempPath, configPath);
};

export default function (pi: ExtensionAPI) {
    let config = loadConfig();

    pi.registerCommand('fast', {
        description: '切换 Fast 模式',
        handler: async (_args, ctx) => {
            const nextConfig = { enabled: !config.enabled };

            try {
                saveConfig(nextConfig);
                config = nextConfig;
                ctx.ui.notify(`Fast 模式已${config.enabled ? '开启' : '关闭'}`, 'info');
            } catch (error) {
                ctx.ui.notify(`保存 Fast 配置失败: ${error}`, 'error');
            }
        },
    });

    pi.on('before_provider_request', (event, ctx) => {
        const model = ctx.model;
        if (
            !config.enabled ||
            model?.provider !== 'openai-codex' ||
            model.api !== 'openai-codex-responses' ||
            !/^gpt-5\.6-(sol|terra|luna)$/.test(model.id) ||
            !event.payload ||
            typeof event.payload !== 'object' ||
            Array.isArray(event.payload)
        ) {
            return;
        }

        return {
            ...(event.payload as Record<string, unknown>),
            service_tier: 'priority',
        };
    });
}
