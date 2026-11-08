import { readdir, readFile } from 'node:fs/promises';

/** Linux process identities prevent delayed cleanup from signalling a reused PID. */
export async function identity(/** @type {number} */ pid) {
    try {
        const stat = await readFile(`/proc/${pid}/stat`, 'utf8');
        const fields = stat.slice(stat.lastIndexOf(')') + 2).split(' ');
        return { pid, parent: Number(fields[1]), group: Number(fields[2]), born: fields[19], zombie: fields[0] === 'Z' };
    } catch { return undefined; }
}

export async function descendants(/** @type {number} */ pid, /** @type {string | undefined} */ born) {
    const entries = await readdir('/proc');
    const records = (await Promise.all(entries.filter(value => /^\d+$/.test(value)).map(value => identity(Number(value)))))
        .filter(value => value !== undefined);
    if (!born || records.find(record => record.pid === pid)?.born !== born) return [];
    const found = new Set([pid]);
    for (let changed = true; changed;) {
        changed = false;
        for (const record of records) if (found.has(record.parent) && !found.has(record.pid)) {
            found.add(record.pid); changed = true;
        }
    }
    return records.filter(record => found.has(record.pid));
}

export async function signalIdentity(/** @type {NonNullable<Awaited<ReturnType<typeof identity>>>} */ record,
    /** @type {NodeJS.Signals} */ signal) {
    const current = await identity(record.pid);
    if (!current || current.born !== record.born || current.zombie || record.pid === process.pid) return;
    try { process.kill(record.pid, signal); }
    catch (error) { if (/** @type {NodeJS.ErrnoException} */ (error).code !== 'ESRCH') throw error; }
}

/** Signal only our PTY group; never inherit/kill the workbench's own process group. */
export async function signalGroup(/** @type {number} */ pid, /** @type {string | undefined} */ born,
    /** @type {NodeJS.Signals} */ signal) {
    if (!born) return;
    const current = await identity(pid);
    if (current && (current.born !== born || current.group !== pid)) return;
    if (pid <= 1 || pid === process.pid) return;
    try { process.kill(-pid, signal); }
    catch (error) { if (/** @type {NodeJS.ErrnoException} */ (error).code !== 'ESRCH') throw error; }
}
