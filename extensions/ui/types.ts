export type TokenUsage = {
    input?: number;
    output?: number;
};

export type FooterData = {
    getGitBranch(): string | null;
    getExtensionStatuses(): ReadonlyMap<string, string>;
    getAvailableProviderCount(): number;
};
