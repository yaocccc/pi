import type { EventBus } from '@earendil-works/pi-coding-agent';

export interface AskQuestionResult {
    answers: string[];
    wasCustom?: boolean;
    customAnswers?: string[];
}

export interface TelegramQuestionOpenRequest {
    toolCallId: string;
    questionId: string;
    registered?: boolean;
}

export interface TelegramQuestionAnswerRequest {
    toolCallId: string;
    questionId: string;
    result: AskQuestionResult;
    accepted?: boolean;
}

export interface TelegramQuestionCancelRequest {
    toolCallId: string;
    accepted?: boolean;
}

export interface TelegramQuestionSettlement {
    toolCallId: string;
    questionId: string;
    winner: 'local' | 'telegram';
}

type TelegramQuestionEntry = {
    questionId: string;
    winner?: TelegramQuestionSettlement['winner'];
    result?: AskQuestionResult;
    listeners: Set<(result: AskQuestionResult) => void>;
    waiters: Set<(result: AskQuestionResult | null) => void>;
};

export const ASK_QUESTION_OPEN_EVENT = 'ask-question:telegram-open';
export const ASK_QUESTION_ANSWER_EVENT = 'ask-question:telegram-answer';
export const ASK_QUESTION_CANCEL_EVENT = 'ask-question:telegram-cancel';
export const ASK_QUESTION_SETTLED_EVENT = 'ask-question:telegram-settled';

export const normalizeAskQuestionOptions = (options: string[]): string[] => {
    const seen = new Set<string>();
    const result: string[] = [];

    for (const option of options) {
        const label = option.replace(/[\r\n\t]+/g, ' ').trim();
        if (!label || seen.has(label)) continue;
        seen.add(label);
        result.push(label);
    }

    return result.slice(0, 8);
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
    Boolean(value) && typeof value === 'object';

/** Owns ask_question state in the ask-question extension and exposes only event-based Telegram IPC. */
export class AskQuestionTelegramBridge {
    private readonly events: EventBus;
    private readonly questions = new Map<string, TelegramQuestionEntry>();
    private readonly detachListeners: Array<() => void>;
    private disposed = false;

    constructor(events: EventBus) {
        this.events = events;
        this.detachListeners = [
            events.on(ASK_QUESTION_OPEN_EVENT, (data) => {
                if (!isRecord(data)
                    || data.registered === true
                    || typeof data.toolCallId !== 'string'
                    || typeof data.questionId !== 'string') return;
                data.registered = this.register(data.toolCallId, data.questionId);
            }),
            events.on(ASK_QUESTION_ANSWER_EVENT, (data) => {
                if (!isRecord(data)
                    || data.accepted === true
                    || typeof data.toolCallId !== 'string'
                    || typeof data.questionId !== 'string'
                    || !isRecord(data.result)
                    || !Array.isArray(data.result.answers)
                    || !data.result.answers.every((answer) => typeof answer === 'string')) return;
                data.accepted = this.settle(
                    data.toolCallId,
                    'telegram',
                    data.questionId,
                    data.result as unknown as AskQuestionResult,
                );
            }),
            events.on(ASK_QUESTION_CANCEL_EVENT, (data) => {
                if (!isRecord(data) || typeof data.toolCallId !== 'string') return;
                const existed = this.questions.has(data.toolCallId);
                this.cancel(data.toolCallId);
                data.accepted = existed;
            }),
        ];
    }

    /** Removes event listeners and closes any still-open Telegram questions. */
    dispose(): void {
        if (this.disposed) return;
        this.disposed = true;
        for (const toolCallId of [...this.questions.keys()]) this.settleLocally(toolCallId);
        this.questions.clear();
        for (const detach of this.detachListeners) detach();
    }

    /** Attaches the executing TUI tool to an already-opened Telegram question. */
    onAnswer(toolCallId: string, listener: (result: AskQuestionResult) => void): () => void {
        const entry = this.questions.get(toolCallId);
        if (!entry) return () => undefined;
        entry.listeners.add(listener);
        if (entry.winner === 'telegram' && entry.result) {
            queueMicrotask(() => {
                if (entry.listeners.has(listener)) listener(entry.result!);
            });
        }
        return () => entry.listeners.delete(listener);
    }

    /** Waits for a registered Telegram question without invoking terminal-only UI. */
    wait(toolCallId: string, signal?: AbortSignal): Promise<AskQuestionResult | null> | undefined {
        const entry = this.questions.get(toolCallId);
        if (!entry) return undefined;
        if (entry.winner) return Promise.resolve(entry.winner === 'telegram' ? entry.result ?? null : null);
        if (signal?.aborted) {
            this.settleLocally(toolCallId);
            return Promise.resolve(null);
        }

        return new Promise((resolve) => {
            let settled = false;
            const finish = (result: AskQuestionResult | null): void => {
                if (settled) return;
                settled = true;
                entry.waiters.delete(finish);
                signal?.removeEventListener('abort', abort);
                resolve(result);
            };
            const abort = (): void => {
                this.settleLocally(toolCallId);
                finish(null);
            };

            entry.waiters.add(finish);
            signal?.addEventListener('abort', abort, { once: true });
        });
    }

    /** Returns false when Telegram already won the one-shot race. */
    settleLocally(toolCallId: string): boolean {
        return this.settle(toolCallId, 'local');
    }

    /** Idempotently settles a pending question locally and releases all bridge state. */
    cancel(toolCallId: string): void {
        this.settleLocally(toolCallId);
        this.questions.delete(toolCallId);
    }

    /** Releases bridge state after ask_question.execute has consumed the winner. */
    release(toolCallId: string): void {
        this.questions.delete(toolCallId);
    }

    private register(toolCallId: string, questionId: string): boolean {
        if (this.disposed || !toolCallId || !questionId || this.questions.has(toolCallId)) return false;
        this.questions.set(toolCallId, {
            questionId,
            listeners: new Set(),
            waiters: new Set(),
        });
        return true;
    }

    private settle(
        toolCallId: string,
        winner: TelegramQuestionSettlement['winner'],
        questionId?: string,
        result?: AskQuestionResult,
    ): boolean {
        const entry = this.questions.get(toolCallId);
        if (!entry) return winner === 'local';
        if (entry.winner || (questionId !== undefined && questionId !== entry.questionId)) return false;

        entry.winner = winner;
        entry.result = result;
        if (winner === 'telegram' && result) {
            for (const listener of [...entry.listeners]) listener(result);
        }
        for (const waiter of [...entry.waiters]) waiter(winner === 'telegram' ? result ?? null : null);
        this.events.emit(ASK_QUESTION_SETTLED_EVENT, {
            toolCallId,
            questionId: entry.questionId,
            winner,
        } satisfies TelegramQuestionSettlement);
        return true;
    }
}
