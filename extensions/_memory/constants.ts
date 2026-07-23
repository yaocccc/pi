import { homedir } from 'node:os';
import { join } from 'node:path';
import { Type } from 'typebox';

export const AGENT_DIR = join(homedir(), '.pi', 'agent');
export const MEMORY_PATH = join(AGENT_DIR, 'memory.md');
export const MEMORY_INDEX_PATH = join(AGENT_DIR, 'memory-index.md');
export const MEMORIES_DIR = join(AGENT_DIR, 'memories');

export const MAX_MEMORY_CHARS = 4_000;
export const MAX_INPUT_CHARS = 6_000;
export const MAX_COMMIT_CHARS = 24_000;
export const MAX_INDEX_CHARS = 30_000;
export const MAX_DETAIL_CHARS = 2_000;
export const DEBOUNCE_MS = 300;

export const DEFAULT_MEMORY = [
    '# Memory\n',
    '> 由 pi memory extension 自动维护。只记录长期稳定、可复用的用户画像；不要记录临时任务、日志、密钥或敏感信息。\n',
    '## 用户画像\n- （暂无）\n',
    '## 偏好\n- （暂无）\n',
    '## 工作环境\n- （暂无）\n',
    '## 交互习惯\n- （暂无）\n',
    '## 待确认\n- （暂无）',
].join('\n');

export const DEFAULT_INDEX = [
    '# Memory Index\n',
    '> This file is the only searchable index for indexed memory.\n',
    '> searchmemory should search this file first.\n',
    '> Do not search `.pi/agent/memory.md`.\n',
    '> Do not scan `.pi/agent/memories/*.md` directly unless an index entry matches.\n',
    '## Entries',
].join('\n');

export const SearchMemoryParams = Type.Object({
    query: Type.String({ description: '当前任务或要搜索的核心记忆关键词。' }),
    project: Type.Optional(Type.String({ description: '可选项目名；默认使用当前工作目录名。' })),
    includeDetails: Type.Optional(Type.Boolean({ description: '是否读取命中的具体 memory 文件；默认 false，只返回索引摘要。最多读取 1-3 个。' })),
});
