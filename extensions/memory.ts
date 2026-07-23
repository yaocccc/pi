import type { ExtensionAPI, ExtensionContext } from '@earendil-works/pi-coding-agent';
import { Box, Text } from '@earendil-works/pi-tui';
import { DEBOUNCE_MS, MEMORY_INDEX_PATH, SearchMemoryParams } from './_memory/constants';
import { commitIndexedMemory } from './_memory/commit';
import {
    compactSearchDisplay,
    duplicateSearchResult,
    ensureIndexedMemory,
    projectName,
    searchCacheKey,
    searchIndexedMemory,
} from './_memory/indexed';
import { memoryPrompt, updateMemory } from './_memory/profile';
import type { SearchCacheEntry } from './_memory/types';
import { textOf, userText } from './_memory/utils';

const memoryExtension = (pi: ExtensionAPI) => {
    let queue = Promise.resolve();
    let pending: string[] = [];
    let timer: NodeJS.Timeout | undefined;
    const searchCaches = new Map<string, Map<string, SearchCacheEntry>>();

    void ensureIndexedMemory().catch(() => undefined);

    pi.registerMessageRenderer('text', (message) => new Text(typeof message.content === 'string' ? message.content : textOf(message.content), 0, 0));
    pi.registerMessageRenderer('indexed-memory', (message) => {
        const box = new Box(1, 1, (text) => `\x1b[48;2;37;37;37m${text}\x1b[49m`);
        box.addChild(new Text(typeof message.content === 'string' ? message.content : textOf(message.content), 0, 0));
        return box;
    });

    const sessionSearchCache = (ctx: ExtensionContext): Map<string, SearchCacheEntry> => {
        const sessionId = ctx.sessionManager.getSessionId();
        let cache = searchCaches.get(sessionId);
        if (!cache) {
            cache = new Map();
            searchCaches.set(sessionId, cache);
        }
        return cache;
    };

    const schedule = (input: string, ctx: ExtensionContext) => {
        pending.push(input);
        if (timer) clearTimeout(timer);
        timer = setTimeout(() => {
            const batch = pending.join('\n\n');
            pending = [];
            queue = queue.catch(() => undefined).then(async () => {
                try {
                    if (await updateMemory(batch, ctx) && ctx.hasUI) ctx.ui.notify('memory.md 已自动更新。', 'info');
                } catch {
                    // 自动更新静默失败，不打断主会话。
                }
            });
        }, DEBOUNCE_MS);
    };

    pi.on('before_agent_start', async (event) => {
        const m = await memoryPrompt();
        return m ? { systemPrompt: `${event.systemPrompt}\n\n${m}` } : undefined;
    });

    pi.on('message_end', (event, ctx) => {
        const text = userText(event.message);
        if (text && text.trim() !== '/end') schedule(text, ctx);
    });

    pi.registerTool({
        name: 'searchmemory',
        label: '搜索核心记忆',
        description: '搜索 indexed memory。只搜索 ~/.pi/agent/memory-index.md；默认只返回索引摘要，只有 includeDetails=true 时才读取命中的 1-3 个具体记忆文件。',
        promptSnippet: '按需调用 searchmemory 搜索核心可索引记忆；不要搜索 memory.md 用户画像，也不要扫描 memories 目录。',
        promptGuidelines: [
            '当用户提到“之前、上次、继续、按之前、和之前一样、还记得”，或任务涉及代码生成、debug、架构决策、依赖版本、已有项目、错误信息、修改已有文件时，应优先调用 searchmemory。',
            'searchmemory 只能搜索 memory-index.md；不要把 memory.md 用户画像当成 indexed memory 搜索目标。',
            '默认只需要 summary；如果 summary 不足，再用 includeDetails=true 读取命中的 1-3 个具体记忆文件。',
            '不要全量读取或扫描 ~/.pi/agent/memories/。',
            'searchmemory 返回的是候选上下文；当它与当前仓库文件冲突时，以当前仓库文件为准。',
        ],
        parameters: SearchMemoryParams,
        async execute(_id, params, _signal, _onUpdate, ctx) {
            const project = projectName(ctx, params.project);
            const includeDetails = params.includeDetails === true;
            const cache = sessionSearchCache(ctx);
            const key = searchCacheKey(params.query, project);
            const cached = cache.get(key);
            if (cached && (cached.hasDetails || !includeDetails)) {
                return { content: [{ type: 'text', text: duplicateSearchResult(cached) }], details: { index: MEMORY_INDEX_PATH, duplicate: true } };
            }

            const result = await searchIndexedMemory(params.query, ctx, includeDetails, project);
            cache.set(key, { query: params.query, project, hasDetails: includeDetails });
            return { content: [{ type: 'text', text: result }], details: { index: MEMORY_INDEX_PATH, duplicate: false } };
        },
        renderResult(result) {
            const first = result.content[0];
            return new Text(first?.type === 'text' ? compactSearchDisplay(first.text) : '', 0, 0);
        },
    });

    pi.registerCommand('end', {
        description: '结束当前任务并沉淀核心 indexed memory',
        handler: async (_args, ctx) => {
            const sendText = (content: string) => pi.sendMessage({ customType: 'text', content, display: true });
            const frames = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
            let frame = 0;
            let progressText = '开始……';
            let progressTimer: NodeJS.Timeout | undefined;
            const renderProgress = () => {
                if (!ctx.hasUI) return;
                ctx.ui.setWidget('memory-progress', [`${frames[frame++ % frames.length]} memory: ${progressText}`]);
            };
            const showProgress = (message: string) => {
                progressText = message;
                renderProgress();
            };

            if (ctx.hasUI) progressTimer = setInterval(renderProgress, 120);
            showProgress('开始……');
            await ctx.waitForIdle();
            try {
                const result = await commitIndexedMemory(ctx, showProgress);
                searchCaches.delete(ctx.sessionManager.getSessionId());
                pi.sendMessage({ customType: 'indexed-memory', content: result, display: true });
            } catch (e) {
                const message = e instanceof Error ? e.message : String(e);
                sendText(`indexed memory 写入失败：${message}`);
            } finally {
                if (progressTimer) clearInterval(progressTimer);
                if (ctx.hasUI) ctx.ui.setWidget('memory-progress', undefined);
            }
        },
    });
};

export default memoryExtension;
