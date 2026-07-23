import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { mkdir, readFile } from 'node:fs/promises';
import { basename, join } from 'node:path';
import { AGENT_DIR, DEFAULT_INDEX, DEFAULT_MEMORY, MAX_DETAIL_CHARS, MEMORIES_DIR, MEMORY_INDEX_PATH, MEMORY_PATH } from './constants';
import type { IndexEntry, MemoryDetail, MemoryType, SearchCacheEntry } from './types';
import { clamp, cleanValue, codeWords, exists, limitSummary, redactSensitive, saveText } from './utils';

export const ensureIndexedMemory = async () => {
    await mkdir(AGENT_DIR, { recursive: true });
    await mkdir(MEMORIES_DIR, { recursive: true });
    if (!await exists(MEMORY_PATH)) await saveText(MEMORY_PATH, DEFAULT_MEMORY);
    if (!await exists(MEMORY_INDEX_PATH)) await saveText(MEMORY_INDEX_PATH, DEFAULT_INDEX);
};

export const readIndex = async (): Promise<string> => {
    await ensureIndexedMemory();
    return (await readFile(MEMORY_INDEX_PATH, 'utf8')).trim() || DEFAULT_INDEX.trim();
};

const MEMORY_TYPES: MemoryType[] = ['solution', 'decision', 'mistake', 'convention', 'note'];

export const normalizeMemoryType = (value: unknown, fallback: MemoryType = 'note'): MemoryType => {
    const text = cleanValue(String(value ?? '')).toLowerCase();
    return MEMORY_TYPES.includes(text as MemoryType) ? text as MemoryType : fallback;
};

const inferMemoryType = (text: string): MemoryType => {
    if (/mistake|踩坑|错误|bug|fix|修复/i.test(text)) return 'mistake';
    if (/convention|约定|偏好|规则|规范/i.test(text)) return 'convention';
    if (/decision|决策|取舍|选择|采用/i.test(text)) return 'decision';
    if (/solution|方案|实现|解决/i.test(text)) return 'solution';
    return 'note';
};

const fallbackWhenToUse = (heading: string, summary: string): string => `未来遇到“${limitSummary(summary || heading)}”相关 coding/debug/架构场景时参考。`;

const constraintsOf = (value: string): string[] => {
    const text = cleanValue(value);
    if (!text || /^none$/i.test(text)) return [];
    return codeWords(text).map(redactSensitive).slice(0, 3);
};

const fieldOf = (body: string, name: string): string => body.match(new RegExp(`^- ${name}:\\s*(.*)$`, 'mi'))?.[1]?.trim() ?? '';
const sectionOf = (markdown: string, name: string): string => markdown.match(new RegExp(`## ${name}\\n([\\s\\S]*?)(?=\\n## |\\s*$)`, 'i'))?.[1]?.trim() ?? '';

export const parseMemoryDetail = (markdown: string): MemoryDetail => {
    const title = markdown.match(/^#\s+(.+)$/m)?.[1]?.trim() ?? '';
    const tags = codeWords(sectionOf(markdown, 'Tags'));
    const summary = cleanValue(sectionOf(markdown, 'Summary'));
    const textForType = [title, sectionOf(markdown, 'Type'), tags.join(' '), summary, sectionOf(markdown, 'Content')].join(' ');
    return {
        title,
        type: normalizeMemoryType(sectionOf(markdown, 'Type'), inferMemoryType(textForType)),
        project: cleanValue(sectionOf(markdown, 'Project')) || 'global',
        tags,
        summary,
        whenToUse: cleanValue(sectionOf(markdown, 'When To Use')),
        content: sectionOf(markdown, 'Content'),
        evidence: sectionOf(markdown, 'Evidence'),
        updated: cleanValue(sectionOf(markdown, 'Updated')) || undefined,
    };
};

export const parseIndex = (markdown: string): IndexEntry[] => {
    const entries: IndexEntry[] = [];
    const re = /(?:^|\n)###\s+(.+)\n([\s\S]*?)(?=\n###\s+|\s*$)/g;
    for (const match of markdown.matchAll(re)) {
        const body = match[2] ?? '';
        const heading = match[1]!.trim();
        const file = cleanValue(fieldOf(body, 'file'));
        if (!file) continue;
        const tags = codeWords(fieldOf(body, 'tags'));
        const keywords = codeWords(fieldOf(body, 'keywords'));
        const summary = cleanValue(fieldOf(body, 'summary'));
        const typeText = [heading, fieldOf(body, 'type'), tags.join(' '), keywords.join(' '), summary].join(' ');
        const whenToUse = cleanValue(fieldOf(body, 'when_to_use') || fieldOf(body, 'whenToUse'));
        entries.push({
            heading,
            file,
            type: normalizeMemoryType(fieldOf(body, 'type'), inferMemoryType(typeText)),
            project: cleanValue(fieldOf(body, 'project')) || 'global',
            tags,
            keywords,
            summary,
            whenToUse: whenToUse || fallbackWhenToUse(heading, summary),
            constraints: constraintsOf(fieldOf(body, 'constraints')),
            updated: cleanValue(fieldOf(body, 'updated')) || undefined,
        });
    }
    return entries;
};

export const renderIndexEntry = (e: IndexEntry): string => [
    `### ${redactSensitive(e.heading)}`,
    '',
    `- file: \`${cleanValue(e.file)}\``,
    `- type: \`${e.type}\``,
    `- project: \`${redactSensitive(e.project)}\``,
    `- tags: ${e.tags.map((t) => `\`${redactSensitive(t)}\``).join(' ')}`,
    `- keywords: ${e.keywords.map((k) => `\`${redactSensitive(k)}\``).join(' ')}`,
    `- summary: ${limitSummary(redactSensitive(e.summary))}`,
    `- when_to_use: ${redactSensitive(e.whenToUse || fallbackWhenToUse(e.heading, e.summary))}`,
    `- constraints: ${e.constraints.length ? e.constraints.slice(0, 3).map((c) => `\`${limitSummary(redactSensitive(c))}\``).join(' ') : 'none'}`,
    ...(e.updated ? [`- updated: \`${cleanValue(e.updated)}\``] : []),
].join('\n');

const numberedHeading = (heading: string, index: number): string => {
    const title = heading.replace(/^\d{4}\s+/, '').trim();
    return `${String(index + 1).padStart(4, '0')} ${title}`;
};

export const renderIndex = (entries: IndexEntry[]): string => `${DEFAULT_INDEX.trim()}\n\n${entries.map((entry, index) => renderIndexEntry({
    ...entry,
    heading: numberedHeading(entry.heading, index),
})).join('\n\n')}`.trimEnd();
export const memoryFileRef = (fileName: string): string => `.pi/agent/memories/${fileName}`;

export const memoryPathFromRef = (file: string): string | undefined => {
    const name = basename(cleanValue(file));
    if (!name || name.includes('..') || !name.endsWith('.md')) return undefined;
    return join(MEMORIES_DIR, name);
};

const normalizeToken = (value: string): string => cleanValue(value).toLowerCase();
const containsToken = (text: string, keyword: string): boolean => normalizeToken(text).includes(keyword);
const hasExact = (values: string[], keyword: string): boolean => values.some((value) => {
    const normalized = normalizeToken(value);
    return normalized === keyword || normalized.split(/[\s,，、]+/).includes(keyword);
});

export const extractKeywords = (query: string): string[] => {
    const words = normalizeToken(query).match(/[\p{L}\p{N}_./:-]+/gu) ?? [];
    return [...new Set(words.map((w) => w.trim()).filter((w) => w.length >= 2).slice(0, 20))];
};

export const projectName = (ctx: ExtensionContext, explicit?: string): string => (explicit?.trim() || basename(ctx.cwd || '') || 'global').toLowerCase();

const scoreEntry = (e: IndexEntry, keywords: string[], project: string): number => {
    let score = 0;
    for (const kw of keywords) {
        if (containsToken(e.heading, kw)) score += 5;
        if (hasExact(e.keywords, kw)) score += 4;
        if (hasExact(e.tags, kw)) score += 3;
        if (containsToken(e.summary, kw)) score += 1;
        if (containsToken(e.whenToUse, kw)) score += 1;
    }
    if (score > 0 && e.project.toLowerCase() === project) score += 3;
    return score;
};

const MIN_SEARCH_SCORE = 3;

const vagueSummary = (entry: IndexEntry): boolean => {
    const summary = entry.summary.trim();
    return summary.length < 18 || (/相关|一些|内容|问题|记录|总结|讨论|临时|杂项|note|todo/i.test(summary) && entry.keywords.length < 4);
};
const temporaryMemory = (entry: IndexEntry, detail: string): boolean => /临时|闲聊|天气|测试|随便|草稿|一次性|未验证|讨论性质|hello/i.test(`${entry.heading} ${entry.summary} ${detail}`);
const coveredMemory = (entry: IndexEntry, detail: string): boolean => /deprecated|archived|废弃|替代|覆盖|不再使用|已被.*覆盖/i.test(`${entry.summary} ${detail}`);
const importantMemory = (entry: IndexEntry, detail: string): boolean => /偏好|preference|convention|约定|solidity|evm|eip-7702|ethers|viem|typescript|go|vue|bug|fix|mistake|踩坑|验证|tsc|安全|权限|gas|代码生成|constraint/i.test(`${entry.tags.join(' ')} ${entry.keywords.join(' ')} ${entry.summary} ${detail}`);
const daysSince = (date?: string): number => date && !Number.isNaN(Date.parse(date)) ? (Date.now() - Date.parse(date)) / 86_400_000 : 9999;

export const memoryValueScore = (entry: IndexEntry, project: string, detail = ''): number => {
    const whenToUse = entry.whenToUse || sectionOf(detail, 'When To Use');
    let score = 20;
    if (entry.project === project || entry.project === 'global') score += 10;
    if (whenToUse) score += 12;
    else score -= 25;
    if (importantMemory(entry, detail)) score += 18;
    if (/验证|通过|修复|resolved|fixed|tsc|test|用户明确|最终/i.test(detail)) score += 10;
    if (vagueSummary(entry)) score -= 18;
    if (temporaryMemory(entry, detail)) score -= 22;
    if (coveredMemory(entry, detail)) score -= 24;
    if (daysSince(entry.updated) > 365 && entry.project !== project && entry.project !== 'global') score -= 12;
    return score;
};

export const searchIndex = (query: string, index: string, project: string, limit = 5): IndexEntry[] => {
    const keywords = extractKeywords(query);
    if (keywords.length === 0) return [];
    return parseIndex(index)
        .map((entry) => ({
            entry,
            relevance: scoreEntry(entry, keywords, project),
            value: memoryValueScore(entry, project),
        }))
        .filter((x) => x.relevance >= MIN_SEARCH_SCORE)
        .sort((a, b) => b.relevance - a.relevance || b.value - a.value)
        .slice(0, limit)
        .map((x) => x.entry);
};

const formatConstraints = (constraints: string[]): string => constraints.length ? constraints.slice(0, 3).map((c) => `\`${limitSummary(c)}\``).join(' ') : 'none';

const formatSearchResults = (results: IndexEntry[], details = new Map<string, string>()): string => {
    if (results.length === 0) return '## Search Memory Results\n\n未找到相关 indexed memory。';
    return [
        '## Search Memory Results',
        '',
        ...results.map((e, i) => [
            `### ${i + 1}. ${e.heading}`,
            `- summary: ${limitSummary(e.summary)}`,
            `- when_to_use: ${e.whenToUse}`,
            `- constraints: ${formatConstraints(e.constraints)}`,
            ...(details.has(e.file) ? ['', '#### Detail', clamp(details.get(e.file)!, MAX_DETAIL_CHARS)] : []),
        ].join('\n')),
    ].join('\n\n');
};

export const compactSearchDisplay = (text: string): string => {
    const headings = [...text.matchAll(/^###\s+(.+)$/gm)].map((m) => `### ${m[1]!.trim()}`);
    if (headings.length > 0) return ['\n## Search Memory Results', ...headings].join('\n');
    if (text.includes('未找到相关 indexed memory')) return '\n## Search Memory Results\n未找到相关 indexed memory。';
    if (text.includes('本会话已搜索过')) return '\n## Search Memory Results\n本会话已搜索过相同 indexed memory query，本次不重复注入结果。';
    return `\n${text.trim()}`;
};

export const searchIndexedMemory = async (query: string, ctx: ExtensionContext, includeDetails = false, explicitProject?: string): Promise<string> => {
    const index = await readIndex();
    const results = searchIndex(query, index, projectName(ctx, explicitProject));
    const details = new Map<string, string>();
    if (includeDetails) {
        for (const entry of results.slice(0, 3)) {
            const path = memoryPathFromRef(entry.file);
            if (!path) continue;
            try {
                details.set(entry.file, redactSensitive(await readFile(path, 'utf8')));
            } catch {
                details.set(entry.file, '（详情文件读取失败或不存在）');
            }
        }
    }
    return formatSearchResults(results, details);
};

export const normalizeSearchQuery = (query: string): string => extractKeywords(query).join(' ') || query.toLowerCase().replace(/\s+/g, ' ').trim();
export const searchCacheKey = (query: string, project: string): string => `${project}::${normalizeSearchQuery(query)}`;

export const duplicateSearchResult = (entry: SearchCacheEntry): string => [
    '## Search Memory Results',
    '',
    '本会话已搜索过相同 indexed memory query，本次不重复注入结果。',
    '',
    `- query: \`${entry.query}\``,
    `- project: \`${entry.project}\``,
    `- previous: \`${entry.hasDetails ? 'includeDetails=true' : 'summary-only'}\``,
    '',
    '请参考上方已有 searchmemory 结果；如需重新检索，请换一个更具体的 query。',
].join('\n');
