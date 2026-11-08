/**
 * Explicit, opt-in metadata channel. Loaded only by the workbench's Pi launch.
 * No message contents, question text, answers or credentials leave the child.
 * @param {{getSessionName:()=>string|undefined, on:(event:string,handler:()=>void)=>void,
 * events:{on:(event:string,handler:(data:unknown)=>void)=>unknown}}} pi
 */
export default function bridge(pi) {
    if (process.env.PI_WORKBENCH_BRIDGE !== '1') return;
    let pending = 0, active = false, last = '';
    const publish = () => {
        if (!active) return;
        const snapshot = JSON.stringify({ version: 1, name: (pi.getSessionName() ?? '').slice(0, 1024), pending });
        if (snapshot === last) return;
        last = snapshot;
        process.stdout.write('\x1b]777;pi-workbench;' + Buffer.from(snapshot).toString('base64') + '\x07');
    };
    pi.events.on('ask-question:pending', data => {
        if (!data || typeof data !== 'object' || !('pending' in data) ||
            !Number.isSafeInteger(data.pending) || Number(data.pending) < 0) return;
        pending = Number(data.pending);
        publish();
    });
    pi.on('session_start', () => { active = true; pending = 0; last = ''; publish(); });
    // Covers /name and asynchronous autoname's pi.setSessionName(), including idle/blocked.
    pi.on('session_info_changed', publish);
    pi.on('session_shutdown', () => { active = false; });
}
