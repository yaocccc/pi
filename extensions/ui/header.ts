import { truncateToWidth, type Component } from '@earendil-works/pi-tui';

export class StartupHeader implements Component {
    constructor(private theme: any) {}

    render(width: number): string[] {
        const logo = [
            '██████╗ ██╗',
            '██╔══██╗██║',
            '██████╔╝██║',
            '██╔═══╝ ██║',
            '██║     ██║',
            '╚═╝     ╚═╝',
        ];
        return [
            '',
            '',
            ...logo.map((text) => this.theme.fg('accent', truncateToWidth(`    ${text}`, width, ''))),
            this.theme.fg('dim', truncateToWidth('    coding agent', width, '')),
        ];
    }

    invalidate() {}
}
