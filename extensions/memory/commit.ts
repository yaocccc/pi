import type { Message } from '@earendil-works/pi-ai';
import { completeSimple } from '@earendil-works/pi-ai/compat';
import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { readFile, readdir, unlink } from 'node:fs/promises';
import { basename, join } from 'node:path';
import { MAX_COMMIT_CHARS, MAX_DETAIL_CHARS, MAX_INDEX_CHARS, MEMORIES_DIR, MEMORY_INDEX_PATH } from './constants';
import { ensureIndexedMemory, extractKeywords, memoryFileRef, memoryPathFromRef, normalizeMemoryType, parseIndex, parseMemoryDetail, projectName, readIndex, renderIndex } from './indexed';
import type { CommitMemory, CompactResult, IndexEntry, Obj, Progress } from './types';
import { asObj, assistantText, clamp, clampTail, cleanValue, limitSummary, redactSensitive, saveText, stripFence, textOf, today } from './utils';

const commitPrompt = (conversation: string, index: string, project: string): string =>
    `你是 indexed memory commit 子 agent。请从本轮会话中提取值得长期保存的核心 coding/debug/架构记忆。\n\n` +
    `规则：\n` +
    `1. /end 不代表一定要写入 memory；没有长期价值时输出 {"memories":[]}。\n` +
    `2. 只保存已解决 bug、技术决策、项目约定、踩坑经验、可复用方案、长期工作流。\n` +
    `3. 不保存闲聊、临时问题、大段日志、未验证猜测、完整代码、无明确复用场景的内容。\n` +
    `4. 简单任务最多 1 条，复杂任务最多 3 条；不要为了凑数强行生成。\n` +
    `5. 每条必须有 summary、keywords、when_to_use 和 file；缺少任一字段就不要保存。\n` +
    `6. 写入前先参考 existing_index；已有类似主题时复用其完整 file，新主题则由你直接生成唯一、简洁、稳定且能表达主题的 .md 文件名。\n` +
    `7. 新文件名不要机械拼接 keywords；推荐 lowercase-kebab-case.md，不要包含目录路径。\n` +
    `8. 不要输出任何私钥、助记词、API key、token、cookie、密码、真实 RPC key；如出现只能写占位符。\n` +
    `9. summary 不超过 120 字，用于索引；content 要比 summary 更详细但保持简短。\n` +
    `10. content 写 4-8 条要点，包含背景、关键决策、实现要点、坑点/约束、验证结果、未来复用方式。\n` +
    `11. evidence 写清来源，例如用户明确要求、修改过的文件、验证命令、错误信息或最终结论。\n` +
    `12. 不要保存完整代码；必要时只保存函数名、文件路径、配置名、命令和关键参数。\n` +
    `13. project 默认使用 ${project}；跨项目通用则使用 global。\n` +
    `14. 只输出 JSON，不要 Markdown，不要解释。\n\n` +
    `JSON 格式：\n` +
    `{"memories":[{"title":"","type":"solution|decision|mistake|convention|note","project":"","tags":[""],"keywords":[""],"summary":"","when_to_use":"","constraints":["最多 3 条，没有则空数组"],"content":"","evidence":"","file":"semantic-memory-name.md"}]}\n\n` +
    `<existing_index>\n${clamp(index, MAX_INDEX_CHARS)}\n</existing_index>\n\n` +
    `<conversation>\n${clampTail(conversation, MAX_COMMIT_CHARS)}\n</conversation>`;

const jsonFromText = (text: string): unknown => {
    const clean = stripFence(text);
    const json = clean.match(/\{[\s\S]*\}/)?.[0] ?? clean;
    return JSON.parse(json);
};

const asStringList = (value: unknown): string[] => {
    if (value === undefined || value === null) return [];
    return Array.isArray(value)
        ? value.map((v) => String(v).trim()).filter(Boolean)
        : String(value).split(/[\s,，、]+/).map((v) => v.trim()).filter(Boolean);
};

const fileNameFromRef = (file: string): string | undefined => {
    const path = memoryPathFromRef(file);
    return path ? basename(path) : undefined;
};

const headingFromFile = (file: string, title: string): string => {
    const id = basename(file).match(/^(\d{4})-/)?.[1];
    return id ? `${id} ${title}` : title;
};

const normalizeCommit = (raw: unknown, fallbackProject: string): CommitMemory | undefined => {
    const obj = asObj(raw);
    if (!obj) return undefined;
    const title = redactSensitive(String(obj.title ?? '').trim());
    const summary = limitSummary(redactSensitive(String(obj.summary ?? '').trim()));
    const content = redactSensitive(String(obj.content ?? '').trim());
    const keywords = asStringList(obj.keywords).map(redactSensitive).slice(0, 12);
    const whenToUse = redactSensitive(String(obj.when_to_use ?? obj.whenToUse ?? '').trim());
    if (!title || !summary || !content || keywords.length === 0 || !whenToUse) return undefined;

    const file = cleanValue(String(obj.file ?? ''));
    return {
        title,
        heading: file ? headingFromFile(file, title) : title,
        file,
        type: normalizeMemoryType(obj.type),
        project: redactSensitive(cleanValue(String(obj.project ?? ''))) || fallbackProject,
        tags: asStringList(obj.tags).map(redactSensitive).slice(0, 8),
        keywords,
        summary,
        whenToUse,
        constraints: asStringList(obj.constraints).map(redactSensitive).slice(0, 3),
        content,
        evidence: redactSensitive(String(obj.evidence ?? '').trim()),
        updated: today(),
    };
};

const titleWithoutId = (heading: string): string => heading.replace(/^\d{4}\s+/, '').trim();
const wordSet = (...values: string[]): Set<string> => new Set(extractKeywords(values.join(' ')));
const commonWords = (a: Set<string>, b: Set<string>): number => [...a].filter((word) => b.has(word)).length;
const commonListWords = (a: string[], b: string[]): number => commonWords(wordSet(...a), wordSet(...b));

const similarityScore = (memory: CommitMemory, entry: IndexEntry): number => {
    const memoryProject = memory.project.toLowerCase();
    const entryProject = entry.project.toLowerCase();
    const sameProject = memoryProject === entryProject;
    const compatibleProject = sameProject || memoryProject === 'global' || entryProject === 'global';
    if (!compatibleProject) return 0;

    const memoryTitle = memory.title.toLowerCase();
    const entryTitle = titleWithoutId(entry.heading).toLowerCase();
    const titleOverlap = commonWords(wordSet(memoryTitle), wordSet(entryTitle));
    const keywordOverlap = commonListWords(memory.keywords, entry.keywords);
    const tagOverlap = commonListWords(memory.tags, entry.tags);
    const summaryOverlap = commonWords(wordSet(memory.summary), wordSet(entry.summary));
    const titleHit = !!memoryTitle && !!entryTitle && (memoryTitle.includes(entryTitle) || entryTitle.includes(memoryTitle));

    let score = sameProject ? 4 : 1;
    if (titleHit) score += 8;
    score += Math.min(6, titleOverlap * 2);
    score += keywordOverlap * 3;
    score += tagOverlap * 2;
    score += Math.min(3, summaryOverlap);
    if (memory.type === entry.type) score += 1;

    const hasTopicSignal = titleHit || keywordOverlap >= 2 || (sameProject && keywordOverlap >= 1 && tagOverlap >= 1) || summaryOverlap >= 4;
    return hasTopicSignal ? score : 0;
};

const similarEntry = (memory: CommitMemory, entries: IndexEntry[]): IndexEntry | undefined => entries
    .map((entry) => ({ entry, score: similarityScore(memory, entry) }))
    .filter((x) => x.score >= 9)
    .sort((a, b) => b.score - a.score)[0]?.entry;

const upsertIndexEntry = (entries: IndexEntry[], next: IndexEntry): IndexEntry[] => {
    const nextFile = fileNameFromRef(next.file);
    const seen = new Set<string>();
    const result: IndexEntry[] = [];
    let inserted = false;
    for (const entry of entries) {
        const file = fileNameFromRef(entry.file);
        if (!file) continue;
        if (nextFile && file === nextFile) {
            if (!inserted) {
                result.push(next);
                seen.add(file);
                inserted = true;
            }
            continue;
        }
        if (seen.has(file)) continue;
        seen.add(file);
        result.push(entry);
    }
    if (!inserted) result.push(next);
    return result;
};

const readDetail = async (entry: IndexEntry): Promise<string> => {
    const path = memoryPathFromRef(entry.file);
    if (!path) return '';
    return readFile(path, 'utf8').catch(() => '');
};

const compactionPrompt = (entries: IndexEntry[], details: Map<string, string>, project: string): string => {
    const index = renderIndex(entries);
    const detailText = entries.map((entry) => [
        `### ${entry.file}`,
        clamp(details.get(entry.file) ?? '（详情不可用）', MAX_DETAIL_CHARS),
    ].join('\n')).join('\n\n');
    return `你是 indexed memory 压缩子 agent。当前索引 ${index.length} 字符，必须压缩到不超过 ${MAX_INDEX_CHARS} 字符。\n\n` +
        `请评估全部记忆的长期复用价值，并决定合并和删除操作。当前项目为 ${project}。\n\n` +
        `规则：\n` +
        `1. 优先合并主题重复、相互补充或同一问题演进过程中的记忆；合并后必须保留全部有效约束、结论、验证结果和复用场景。\n` +
        `2. 仅删除临时、已废弃、已被完整覆盖、缺少复用价值或明显重复的记忆。\n` +
        `3. 优先保留当前项目和 global 记忆、明确 when_to_use、经过验证的方案、安全约束、用户明确约定及近期内容。\n` +
        `4. 不要合并不兼容项目的专属约定；不要删除唯一的关键技术决策、安全规则或踩坑结论。\n` +
        `5. merge 的 keep_file 和 drop_files 必须来自现有 file；drop_files 内容必须完整吸收到 memory。\n` +
        `6. memory 的 summary 不超过 120 字，content 保持 4-10 条简洁要点，不输出完整代码。\n` +
        `7. 操作应足以把索引降到 ${MAX_INDEX_CHARS} 字符以内，但不要过度删除。\n` +
        `8. 只输出 JSON，不要 Markdown，不要解释。\n\n` +
        `JSON 格式：\n` +
        `{"merges":[{"keep_file":".pi/agent/memories/0001-example.md","drop_files":[".pi/agent/memories/0002-example.md"],"memory":{"title":"","type":"solution|decision|mistake|convention|note","project":"","tags":[""],"keywords":[""],"summary":"","when_to_use":"","constraints":[],"content":"","evidence":""}}],"deletes":[".pi/agent/memories/0003-example.md"]}\n\n` +
        `<index>\n${index}\n</index>\n\n` +
        `<details>\n${detailText}\n</details>`;
};

const requestCompactionPlan = async (entries: IndexEntry[], details: Map<string, string>, project: string, ctx: ExtensionContext): Promise<Obj | undefined> => {
    if (!ctx.model) return undefined;
    const auth = await ctx.modelRegistry.getApiKeyAndHeaders(ctx.model);
    if (!auth.ok || !auth.apiKey) return undefined;
    const messages: Message[] = [{ role: 'user', content: [{ type: 'text', text: compactionPrompt(entries, details, project) }], timestamp: Date.now() }];
    const res = await completeSimple(ctx.model, { messages }, {
        apiKey: auth.apiKey,
        headers: auth.headers,
        maxTokens: 6_000,
        reasoning: 'medium',
    });
    if (res.stopReason === 'error') return undefined;
    try {
        return asObj(jsonFromText(assistantText(res)));
    } catch {
        return undefined;
    }
};

const compactIndexedMemories = async (entries: IndexEntry[], project: string, ctx: ExtensionContext): Promise<CompactResult> => {
    const details = new Map<string, string>();
    for (const entry of entries) details.set(entry.file, await readDetail(entry));

    let kept = [...entries];
    const removed: IndexEntry[] = [];
    const removedFiles = new Set<string>();
    const warnings: string[] = [];
    let merged = 0;
    const removeByName = (fileName: string): IndexEntry | undefined => {
        const entry = kept.find((item) => fileNameFromRef(item.file) === fileName);
        if (!entry) return undefined;
        kept = kept.filter((item) => fileNameFromRef(item.file) !== fileName);
        if (!removedFiles.has(fileName)) {
            removedFiles.add(fileName);
            removed.push(entry);
        }
        return entry;
    };

    for (let round = 1; renderIndex(kept).length > MAX_INDEX_CHARS && round <= 3; round++) {
        const plan = await requestCompactionPlan(kept, details, project, ctx);
        if (!plan) {
            warnings.push(`第 ${round} 轮模型压缩评估失败，已停止压缩。`);
            break;
        }

        let changed = false;
        const touched = new Set<string>();
        const rawMerges = Array.isArray(plan.merges) ? plan.merges : [];
        for (const raw of rawMerges) {
            const merge = asObj(raw);
            const keepFileName = fileNameFromRef(String(merge?.keep_file ?? merge?.keepFile ?? ''));
            const keepEntry = keepFileName ? kept.find((entry) => fileNameFromRef(entry.file) === keepFileName) : undefined;
            const dropFileNames = [...new Set(asStringList(merge?.drop_files ?? merge?.dropFiles)
                .map((file) => fileNameFromRef(file))
                .filter((file): file is string => !!file && file !== keepFileName && !touched.has(file)))];
            const dropEntries = dropFileNames.map((file) => kept.find((entry) => fileNameFromRef(entry.file) === file)).filter((entry): entry is IndexEntry => !!entry);
            const memory = normalizeCommit(merge?.memory, keepEntry?.project || project);
            if (!keepFileName || !keepEntry || touched.has(keepFileName) || dropEntries.length === 0 || !memory) continue;

            memory.file = memoryFileRef(keepFileName);
            memory.heading = headingFromFile(memory.file, memory.title);
            const mergedMarkdown = memoryMarkdown(memory);
            await saveText(join(MEMORIES_DIR, keepFileName), mergedMarkdown);
            details.set(memory.file, mergedMarkdown);
            const nextEntry: IndexEntry = {
                heading: memory.heading,
                file: memory.file,
                type: memory.type,
                project: memory.project,
                tags: memory.tags,
                keywords: memory.keywords,
                summary: memory.summary,
                whenToUse: memory.whenToUse,
                constraints: memory.constraints,
                updated: memory.updated,
            };
            kept = upsertIndexEntry(kept, nextEntry);
            touched.add(keepFileName);
            for (const drop of dropEntries) {
                const dropFileName = fileNameFromRef(drop.file);
                if (!dropFileName) continue;
                removeByName(dropFileName);
                touched.add(dropFileName);
                merged++;
            }
            changed = true;
        }

        for (const file of asStringList(plan.deletes)) {
            const fileName = fileNameFromRef(file);
            if (!fileName || touched.has(fileName)) continue;
            if (removeByName(fileName)) {
                touched.add(fileName);
                changed = true;
            }
        }

        if (!changed) {
            warnings.push(`第 ${round} 轮模型未返回有效的合并或删除操作，已停止压缩。`);
            break;
        }
    }

    for (const entry of removed) {
        const path = memoryPathFromRef(entry.file);
        if (path) await unlink(path).catch(() => undefined);
    }
    if (renderIndex(kept).length > MAX_INDEX_CHARS) warnings.push(`模型压缩后索引仍为 ${renderIndex(kept).length} 字符，超过 ${MAX_INDEX_CHARS} 字符上限。`);
    return { entries: kept, removed, merged, warnings };
};

type ConsistencyResult = { entries: IndexEntry[]; warnings: string[] };

const titleKey = (heading: string): string => titleWithoutId(heading).toLowerCase().replace(/[^\p{L}\p{N}]+/gu, ' ').trim();
const warningLines = (warnings: string[]): string[] => warnings.length ? [
    '',
    '### Warnings',
    ...warnings.slice(0, 8).map((warning) => `- ${warning}`),
    ...(warnings.length > 8 ? [`- 还有 ${warnings.length - 8} 条 warning 已省略。`] : []),
] : [];

const validateIndexedMemory = async (entries: IndexEntry[]): Promise<ConsistencyResult> => {
    const warnings: string[] = [];
    const seenFiles = new Set<string>();
    const seenTitles = new Map<string, string>();
    const fixed: IndexEntry[] = [];

    for (const entry of entries) {
        const fileName = fileNameFromRef(entry.file);
        if (!fileName) {
            warnings.push(`移除非法 file 引用：${entry.file || '(empty)'}`);
            continue;
        }
        if (seenFiles.has(fileName)) {
            warnings.push(`移除重复 file 的 index 项：${entry.file}`);
            continue;
        }

        const file = memoryFileRef(fileName);
        const path = memoryPathFromRef(file);
        if (!path) {
            warnings.push(`移除无法解析路径的 index 项：${entry.file}`);
            continue;
        }

        let detail = '';
        try {
            detail = await readFile(path, 'utf8');
        } catch {
            warnings.push(`移除缺失详情文件的 index 项：${file}`);
            continue;
        }

        const redactedDetail = redactSensitive(detail);
        if (redactedDetail !== detail) {
            await saveText(path, redactedDetail);
            warnings.push(`已过滤详情文件中的敏感信息：${file}`);
        }

        const meta = parseMemoryDetail(redactedDetail);
        const heading = redactSensitive(entry.heading).trim() || headingFromFile(file, meta.title || 'Untitled Memory');
        const summary = limitSummary(redactSensitive(entry.summary || meta.summary || titleWithoutId(heading)));
        const whenToUse = redactSensitive(entry.whenToUse || meta.whenToUse || `未来遇到“${summary}”相关 coding/debug/架构场景时参考。`);
        const tags = (entry.tags.length ? entry.tags : meta.tags).map(redactSensitive).filter(Boolean).slice(0, 8);
        const keywords = (entry.keywords.length ? entry.keywords : extractKeywords([heading, summary, whenToUse, ...tags].join(' '))).map(redactSensitive).filter(Boolean).slice(0, 12);
        const fixedEntry: IndexEntry = {
            ...entry,
            heading,
            file,
            project: redactSensitive(entry.project || meta.project || 'global'),
            tags,
            keywords: keywords.length ? keywords : ['memory'],
            summary: summary || titleWithoutId(heading),
            whenToUse,
            constraints: entry.constraints.map(redactSensitive).filter(Boolean).slice(0, 3),
            updated: entry.updated ?? meta.updated,
        };

        if (!entry.summary && !meta.summary) warnings.push(`已为缺失 summary 的 index 项生成摘要：${file}`);
        if (entry.keywords.length === 0) warnings.push(`已为缺失 keywords 的 index 项生成关键词：${file}`);
        if (!entry.whenToUse && !meta.whenToUse) warnings.push(`已为缺失 when_to_use 的 index 项生成使用场景：${file}`);

        const key = titleKey(fixedEntry.heading);
        const firstFile = key ? seenTitles.get(key) : undefined;
        if (key && firstFile) warnings.push(`发现明显重复 title：${firstFile} 与 ${file}`);
        else if (key) seenTitles.set(key, file);

        seenFiles.add(fileName);
        fixed.push(fixedEntry);
    }

    const indexedFiles = new Set(fixed.map((entry) => fileNameFromRef(entry.file)).filter((file): file is string => !!file));
    const files = await readdir(MEMORIES_DIR).catch(() => []);
    for (const file of files.filter((name) => name.endsWith('.md')).sort()) {
        if (!indexedFiles.has(file)) warnings.push(`发现孤儿 memory 文件：.pi/agent/memories/${file}`);
    }

    return { entries: fixed, warnings };
};

const safeValidateIndexedMemory = async (entries: IndexEntry[]): Promise<ConsistencyResult> => {
    try {
        return await validateIndexedMemory(entries);
    } catch (e) {
        const message = e instanceof Error ? e.message : String(e);
        return { entries, warnings: [`indexed memory 一致性校验失败：${message}`] };
    }
};

const memoryMarkdown = (m: CommitMemory): string => redactSensitive([
    `# ${m.title}`,
    '',
    '## Type',
    m.type,
    '',
    '## Project',
    m.project,
    '',
    '## Tags',
    m.tags.map((t) => `\`${t}\``).join(' '),
    '',
    '## Summary',
    limitSummary(m.summary),
    '',
    '## When To Use',
    m.whenToUse || '未来遇到相同或相近 coding/debug/架构场景时参考。',
    '',
    '## Content',
    clamp(m.content, 4_000),
    '',
    '## Evidence',
    m.evidence || '来自 /end 时对当前会话的总结。',
    '',
    '## Updated',
    m.updated ?? today(),
].join('\n'));

const sessionConversation = (ctx: ExtensionContext): string => {
    const leaf = ctx.sessionManager.getLeafId();
    const entries = leaf ? ctx.sessionManager.getBranch(leaf) : ctx.sessionManager.getEntries();
    return entries.map((entry: any) => {
        if (entry.type !== 'message') return '';
        const role = entry.message?.role;
        if (role !== 'user' && role !== 'assistant') return '';
        const text = textOf(entry.message.content);
        return text ? `[${role}]\n${redactSensitive(text)}` : '';
    }).filter(Boolean).join('\n\n');
};

export const commitIndexedMemory = async (ctx: ExtensionContext, progress: Progress = () => undefined): Promise<string> => {
    progress('准备记忆文件……');
    await ensureIndexedMemory();
    if (!ctx.model) return '## Indexed Memory Commit\n\n当前没有可用模型，未写入 indexed memory。';
    const auth = await ctx.modelRegistry.getApiKeyAndHeaders(ctx.model);
    if (!auth.ok || !auth.apiKey) return '## Indexed Memory Commit\n\n当前模型认证不可用，未写入 indexed memory。';

    const index = await readIndex();
    const project = projectName(ctx);
    const conversation = sessionConversation(ctx);
    if (!conversation.trim()) return '## Indexed Memory Commit\n\n当前会话没有可总结内容。';

    progress('总结当前会话……');
    const messages: Message[] = [{ role: 'user', content: [{ type: 'text', text: commitPrompt(conversation, index, project) }], timestamp: 0 }];
    const res = await completeSimple(ctx.model, { messages }, {
        apiKey: auth.apiKey,
        headers: auth.headers,
        maxTokens: 4_000,
        reasoning: 'medium',
    });
    if (res.stopReason === 'error') return '## Indexed Memory Commit\n\n模型总结失败，未写入 indexed memory。';

    let parsed: unknown;
    try {
        parsed = jsonFromText(assistantText(res));
    } catch {
        return '## Indexed Memory Commit\n\n模型输出不是有效 JSON，未写入 indexed memory。';
    }
    const parsedObj = asObj(parsed);
    const rawMemories = Array.isArray(parsedObj?.memories) ? parsedObj.memories as unknown[] : [];
    let entries = parseIndex(index);
    const committed: Array<{ memory: CommitMemory; action: '新增' | '更新' }> = [];
    const diskFiles = await readdir(MEMORIES_DIR).catch(() => []);
    const reservedFiles = new Set([...diskFiles, ...entries.map((entry) => fileNameFromRef(entry.file)).filter((file): file is string => !!file)]);
    const createdFiles = new Set<string>();
    let compacted: CompactResult | undefined;
    if (rawMemories.length > 0) progress('写入记忆……');

    for (const raw of rawMemories.slice(0, 3)) {
        const item = normalizeCommit(raw, project);
        if (!item) continue;

        const requestedFileName = fileNameFromRef(item.file);
        const requestedMatch = requestedFileName && !createdFiles.has(requestedFileName)
            ? entries.find((entry) => fileNameFromRef(entry.file) === requestedFileName)
            : undefined;
        const matched = requestedMatch ?? similarEntry(item, entries);
        const matchedFileName = matched ? fileNameFromRef(matched.file) : undefined;
        const fileName = matchedFileName ?? requestedFileName;
        if (!fileName || (!matched && reservedFiles.has(fileName))) continue;
        if (!matched) {
            reservedFiles.add(fileName);
            createdFiles.add(fileName);
        }
        item.file = memoryFileRef(fileName);
        item.heading = headingFromFile(item.file, item.title);

        await saveText(join(MEMORIES_DIR, fileName), memoryMarkdown(item));
        const oldIndex = entries.findIndex((e) => fileNameFromRef(e.file) === fileName);
        const nextEntry: IndexEntry = {
            heading: item.heading,
            file: item.file,
            type: item.type,
            project: item.project,
            tags: item.tags,
            keywords: item.keywords,
            summary: item.summary,
            whenToUse: item.whenToUse,
            constraints: item.constraints,
            updated: item.updated,
        };
        const action = oldIndex >= 0 ? '更新' : '新增';
        entries = upsertIndexEntry(entries, nextEntry);
        committed.push({ memory: item, action });
    }

    const needsValidation = committed.length > 0 || renderIndex(entries).length > MAX_INDEX_CHARS;
    if (!needsValidation) return '## Indexed Memory Commit\n\n本轮无 indexed memory 写入。';

    progress('校验记忆索引……');
    const validation = await safeValidateIndexedMemory(entries);
    entries = validation.entries;

    if (renderIndex(entries).length > MAX_INDEX_CHARS) {
        progress('压缩记忆……');
        compacted = await compactIndexedMemories(entries, project, ctx);
        entries = compacted.entries;
    }

    const renderedIndex = renderIndex(entries);
    await saveText(MEMORY_INDEX_PATH, renderedIndex);
    progress('完成。');
    if (committed.length === 0 && compacted) {
        return [
            '## Indexed Memory Commit',
            '',
            '本轮无 indexed memory 写入。',
            `已压缩 indexed memory：合并 ${compacted.merged} 条，删除 ${compacted.removed.length} 条，当前索引 ${renderedIndex.length} 字符（上限 ${MAX_INDEX_CHARS}）。`,
            ...warningLines([...validation.warnings, ...compacted.warnings]),
        ].join('\n');
    }
    return [
        '## Indexed Memory Commit',
        '',
        `已处理 ${committed.length} 条 indexed memory：新增 ${committed.filter((x) => x.action === '新增').length} 条，更新 ${committed.filter((x) => x.action === '更新').length} 条。`,
        '',
        ...committed.map(({ memory: m, action }, i) => `${i + 1}. ${action}：${m.title}\n   - file: \`${m.file}\`\n   - summary: ${m.summary}\n   - when_to_use: ${m.whenToUse}\n   - content:\n${clamp(m.content, 800).split('\n').map((line) => `     ${line}`).join('\n')}`),
        ...(compacted ? ['', `压缩结果：合并 ${compacted.merged} 条，删除 ${compacted.removed.length} 条，当前索引 ${renderedIndex.length} 字符（上限 ${MAX_INDEX_CHARS}）。`] : []),
        ...warningLines([...validation.warnings, ...(compacted?.warnings ?? [])]),
    ].join('\n');
};
