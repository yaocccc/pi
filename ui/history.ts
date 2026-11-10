import { getLatestCompactionEntry, InteractiveMode, type AgentSessionEvent, type SessionManager, type SessionEntry } from '@earendil-works/pi-coding-agent';

const PATCH = Symbol.for('pi.extensions.ui.history.v1');
type HistoryMode = {
    sessionManager: Pick<SessionManager, 'getBranch'>;
    isInitialized: boolean;
    chatContainer: { clear(): void };
    entriesRenderedByBoundaryCompaction: Set<string>;
    renderSessionEntries(entries: SessionEntry[], options?: { updateFooter?: boolean; populateHistory?: boolean }): void;
    handleEvent(event: AgentSessionEvent): Promise<void>;
    [PATCH]?: boolean;
};

export const patchSessionHistory = (): void => {
    const prototype = InteractiveMode.prototype as unknown as HistoryMode;
    if (prototype[PATCH]) return;
    const render = prototype.renderSessionEntries;
    const handleEvent = prototype.handleEvent;
    prototype.renderSessionEntries = function (entries, options) {
        // Initial load and rebuild pass the latest compaction followed by retained context.
        // Render the current branch instead, without changing the model's context projection.
        if (entries[0]?.type === 'compaction') {
            const branch = this.sessionManager.getBranch();
            if (entries[0].id === getLatestCompactionEntry(branch)?.id) entries = branch;
        }
        render.call(this, entries, options);
    };
    prototype.handleEvent = function (event) {
        const completed = event.type === 'compaction_end' && !event.aborted && !!event.result;
        const boundary = event.type === 'entry_appended' && event.entry.type === 'compaction'
            && !this.entriesRenderedByBoundaryCompaction.has(event.entry.id)
            && getLatestCompactionEntry(this.sessionManager.getBranch())?.id === event.entry.id;
        const initialized = this.isInitialized;
        const result = handleEvent.call(this, event);
        if (completed || boundary) {
            // Native compaction handlers synchronously rebuild in pieces and append their summary.
            // Replay once in branch order before the queued redraw, preserving native lifecycle and
            // boundary-event deduplication. One render also matches tool results across the boundary.
            const restore = () => {
                this.chatContainer.clear();
                render.call(this, this.sessionManager.getBranch());
            };
            // Before initialization the native handler awaits init() before processing its event.
            if (!initialized) return result.then(restore);
            restore();
        }
        return result;
    };
    prototype[PATCH] = true;
};
