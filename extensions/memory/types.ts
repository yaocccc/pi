export type Obj = Record<string, unknown>;

export type MemoryType = 'solution' | 'decision' | 'mistake' | 'convention' | 'note';

export type IndexEntry = {
    heading: string;
    file: string;
    type: MemoryType;
    project: string;
    tags: string[];
    keywords: string[];
    summary: string;
    whenToUse: string;
    constraints: string[];
    updated?: string;
};

export type MemoryDetail = {
    title: string;
    type: MemoryType;
    project: string;
    tags: string[];
    summary: string;
    whenToUse: string;
    content: string;
    evidence: string;
    updated?: string;
};

export type CommitMemory = IndexEntry & {
    title: string;
    content: string;
    evidence: string;
};

export type ProgressInfo = {
    contextTokens?: number;
    outputTokens?: number;
    model?: string;
    thinking?: string;
    cancellable?: boolean;
};

export type Progress = (message: string, info?: ProgressInfo) => void;

export type CompactResult = {
    entries: IndexEntry[];
    removed: IndexEntry[];
    merged: number;
    warnings: string[];
};

export type SearchCacheEntry = {
    query: string;
    project: string;
};

export type GetMemoryResult = {
    text: string;
    name: string;
    file?: string;
    found: boolean;
    version?: string;
};
