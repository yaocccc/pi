import { withFileMutationQueue, type ExtensionContext } from '@earendil-works/pi-coding-agent';
import { readFile, rename, unlink } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { asObj, saveText } from './utils';

type SummaryRequest = {
    sessionId: string;
    leafId?: string;
    requestedAt: string;
};

export type ClaimedSummaryRequest = {
    request: SummaryRequest;
    path: string;
};

const requestDir = join(tmpdir(), 'pi-memory-summarize');
const safeSessionId = (ctx: ExtensionContext): string => ctx.sessionManager.getSessionId().replace(/[^a-zA-Z0-9_-]/g, '_');
const pendingPath = (ctx: ExtensionContext): string => join(requestDir, `${safeSessionId(ctx)}.pending.json`);
const processingPath = (ctx: ExtensionContext): string => join(requestDir, `${safeSessionId(ctx)}.processing.json`);
const remove = async (path: string) => unlink(path).catch(() => undefined);

export const queueSummaryRequest = async (ctx: ExtensionContext): Promise<void> => {
    const path = pendingPath(ctx);
    const request: SummaryRequest = {
        sessionId: ctx.sessionManager.getSessionId(),
        leafId: ctx.sessionManager.getLeafId() ?? undefined,
        requestedAt: new Date().toISOString(),
    };
    await withFileMutationQueue(path, () => saveText(path, JSON.stringify(request)));
};

export const claimSummaryRequest = async (ctx: ExtensionContext): Promise<ClaimedSummaryRequest | undefined> => {
    const pending = pendingPath(ctx);
    const processing = processingPath(ctx);
    await remove(processing);
    try {
        await rename(pending, processing);
    } catch (e) {
        if ((e as NodeJS.ErrnoException).code === 'ENOENT') return undefined;
        throw e;
    }

    try {
        const parsed = asObj(JSON.parse(await readFile(processing, 'utf8')));
        if (parsed?.sessionId !== ctx.sessionManager.getSessionId()) {
            await remove(processing);
            return undefined;
        }
        return {
            request: {
                sessionId: String(parsed.sessionId),
                leafId: typeof parsed.leafId === 'string' ? parsed.leafId : undefined,
                requestedAt: typeof parsed.requestedAt === 'string' ? parsed.requestedAt : '',
            },
            path: processing,
        };
    } catch {
        await remove(processing);
        return undefined;
    }
};

export const finishSummaryRequest = async (claimed: ClaimedSummaryRequest): Promise<void> => remove(claimed.path);

export const clearSummaryRequest = async (ctx: ExtensionContext): Promise<void> => {
    await Promise.all([remove(pendingPath(ctx)), remove(processingPath(ctx))]);
};
