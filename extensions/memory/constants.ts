import { homedir } from 'node:os';
import { join } from 'node:path';
import { Type } from 'typebox';

export const AGENT_DIR = join(homedir(), '.pi', 'agent');
export const MEMORY_INDEX_PATH = join(AGENT_DIR, 'memory-index.md');
export const MEMORY_SETTINGS_PATH = join(AGENT_DIR, 'memory-settings.json');
export const MEMORIES_DIR = join(AGENT_DIR, 'memories');

export const MAX_COMMIT_CHARS = 24_000;
export const MAX_MERGE_DETAIL_CHARS = 6_000;
export const MAX_INDEX_CHARS = 30_000;
export const MAX_DETAIL_CHARS = 2_000;
export const MAX_GET_MEMORY_DETAIL_CHARS = 6_000;
export const DEFAULT_INDEX = [
    '# Memory Index\n',
    '> This file is the only searchable index for indexed memory.\n',
    '> memory_search should search this file first.\n',
    '> Do not scan `.pi/agent/memories/*.md` directly unless an index entry matches.\n',
    '## Entries',
].join('\n');

export const MemorySearchParams = Type.Object({
    query: Type.String({ description: '搜索关键词。' }),
    project: Type.Optional(Type.String({ description: '项目名；默认使用当前目录名。' })),
});

export const MemoryGetParams = Type.Object({
    name: Type.String({ description: 'memory_search 返回的记忆名称，不包含开头的编号。', minLength: 1 }),
});

export const MemorySummarizeParams = Type.Object({});
