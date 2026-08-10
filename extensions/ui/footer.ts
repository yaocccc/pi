import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { truncateToWidth, visibleWidth, type Component } from '@earendil-works/pi-tui';
import type { FooterData } from './types.ts';

export class NoCostFooter implements Component {
    constructor(
        private ctx: ExtensionContext,
        private theme: any,
        private footerData: FooterData,
    ) {}

    render(width: number): string[] {
        const home = process.env.HOME || process.env.USERPROFILE;
        const branch = this.footerData.getGitBranch();
        let pwd = this.ctx.cwd;
        if (home && pwd.startsWith(home)) pwd = `~${pwd.slice(home.length)}`;
        if (branch) pwd += ` (${branch})`;
        const usage = this.ctx.getContextUsage?.();
        const contextText = usage ? `${usage.percent?.toFixed?.(1) ?? '?'}%` : '';

        const model = this.ctx.model as any;
        const modelText = model?.name || model?.id || 'no-model';
        const thinkingLevel = this.ctx.thinkingLevel || 'off';
        const rightText = `${modelText} . ${thinkingLevel}`;
        const statuses = Array.from(this.footerData.getExtensionStatuses().values()).map((s) => s.replace(/[\r\n\t]/g, ' ').trim()).filter(Boolean);
        const leftText = [pwd, statuses.join(' · '), contextText].filter(Boolean).join(' · ');
        const right = this.theme.fg('dim', rightText);
        const rightWidth = visibleWidth(right);
        const minGap = 2;

        if (rightWidth + minGap >= width) return [truncateToWidth(right, width, '')];

        const availableLeft = width - rightWidth - minGap;
        const left = truncateToWidth(this.theme.fg('dim', leftText), availableLeft, this.theme.fg('dim', '…'));
        const padding = ' '.repeat(Math.max(minGap, width - visibleWidth(left) - rightWidth));
        return [left + padding + right];
    }

    invalidate() {}
}
