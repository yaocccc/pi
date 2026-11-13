import { estimateTokens } from "@earendil-works/pi-coding-agent";

type ToolResult = Pick<Extract<Parameters<typeof estimateTokens>[0], { role: 'toolResult' }>, 'content'> & { isError?: boolean };

// Historical payload size, not live retained context or provider billing usage.
export const estimateToolTokens = (args: unknown, result: ToolResult): number =>
    Math.ceil((JSON.stringify(args ?? {})?.length ?? 0) / 4) + estimateTokens({
        role: 'toolResult', toolCallId: '', toolName: '', content: result.content,
        isError: result.isError ?? false, timestamp: 0,
    });
