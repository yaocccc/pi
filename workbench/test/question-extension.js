// Explicitly loaded only by offline smoke tests; no model or tool calls.
export default function questionFixture(pi) {
    pi.registerCommand('workbench-smoke-question', {
        description: 'Offline workbench input-isolation probe',
        handler: async (_args, ctx) => {
            const answer = await ctx.ui.select('Workbench offline question', ['Alpha', 'Beta']);
            ctx.ui.notify(`Workbench answer: ${answer ?? 'cancelled'}`, 'info');
        },
    });
}
