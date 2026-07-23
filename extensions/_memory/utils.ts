import { access, mkdir, rename, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';
import type { Obj } from './types';

export const asObj = (v: unknown): Obj | undefined => v && typeof v === 'object' ? v as Obj : undefined;
export const clamp = (s: string, n: number): string => s.trim().length <= n ? s.trim() : `${s.trim().slice(0, n)}\n……（已截断）`;
export const clampTail = (s: string, n: number): string => s.trim().length <= n ? s.trim() : `……（前文已截断）\n${s.trim().slice(-n)}`;
export const today = (): string => new Date().toISOString().slice(0, 10);
export const stripFence = (s: string): string => s.trim().replace(/^```(?:json|markdown|md)?\s*\n([\s\S]*?)\n```$/i, '$1').trim();
export const limitSummary = (s: string): string => s.trim().length <= 120 ? s.trim() : `${s.trim().slice(0, 120)}…`;

export const textOf = (content: unknown): string => {
    if (typeof content === 'string') return content.trim();
    if (!Array.isArray(content)) return '';
    return content.map((p) => {
        const b = asObj(p);
        return b?.type === 'text' && typeof b.text === 'string' ? b.text : '';
    }).filter(Boolean).join('\n').trim();
};

export const userText = (message: unknown): string | undefined => {
    const msg = asObj(message);
    if (msg?.role !== 'user') return undefined;
    return textOf(msg.content) || undefined;
};

export const assistantText = (message: { content: unknown[] }): string => message.content.map((p) => {
    const b = asObj(p);
    return b?.type === 'text' && typeof b.text === 'string' ? b.text : '';
}).filter(Boolean).join('\n').trim();

export const redactSensitive = (text: string): string => text
    .replace(/-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----/g, '<PRIVATE_KEY>')
    .replace(/\b(?:0x)?[a-fA-F0-9]{64}\b/g, '<PRIVATE_KEY>')
    .replace(/\b(mnemonic|seed phrase|seed|recovery phrase)\s*[:=]\s*[^\n]+/gi, '$1=<MNEMONIC>')
    .replace(/\b(api[_-]?key|apikey|client[_-]?secret)\s*[:=]\s*[^\s`'"，。；]+/gi, '$1=<API_KEY>')
    .replace(/\b(access[_-]?token|refresh[_-]?token|id[_-]?token|token|cookie|session)\s*[:=]\s*[^\s`'"，。；]+/gi, '$1=<TOKEN>')
    .replace(/\b(password|passwd|pwd|db[_-]?password)\s*[:=]\s*[^\s`'"，。；]+/gi, '$1=<PASSWORD>')
    .replace(/\b(secret|exchange[_-]?secret|signing[_-]?secret)\s*[:=]\s*[^\s`'"，。；]+/gi, '$1=<SECRET>')
    .replace(/\bBearer\s+[a-zA-Z0-9._-]{20,}\b/g, 'Bearer <TOKEN>')
    .replace(/\beyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\b/g, '<TOKEN>')
    .replace(/\b(rpc|rpc_url|rpc[_-]?key)\s*[:=]\s*[^\s`'"，。；]+/gi, '$1=<RPC_URL>')
    .replace(/\b(?:postgres|postgresql|mysql|mongodb|redis):\/\/[^\s`'"，。；]+/gi, '<SECRET>')
    .replace(/https?:\/\/[^\s`'"，。；]*(?:infura|alchemy|quicknode|ankr|blastapi|drpc|rpc)[^\s`'"，。；]*/gi, '<RPC_URL>');

export const cleanValue = (s: string | undefined): string => (s ?? '').trim().replace(/^`|`$/g, '').trim();

export const codeWords = (s: string): string[] => {
    const ticks = [...s.matchAll(/`([^`]+)`/g)].map((m) => m[1]!.trim()).filter(Boolean);
    return ticks.length ? ticks : s.split(/[\s,，、]+/).map(cleanValue).filter(Boolean);
};

export const saveText = async (path: string, content: string) => {
    await mkdir(dirname(path), { recursive: true });
    const tmp = `${path}.${process.pid}.${Date.now()}.tmp`;
    await writeFile(tmp, content.trimEnd() + '\n');
    await rename(tmp, path);
};

export const exists = async (path: string): Promise<boolean> => {
    try {
        await access(path);
        return true;
    } catch (e) {
        if ((e as NodeJS.ErrnoException).code === 'ENOENT') return false;
        throw e;
    }
};
