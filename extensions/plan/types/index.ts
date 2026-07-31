export interface CheckResult {
    status: 'pass' | 'fail';
    reason?: string;
    improvements?: string[];
}

export type PlanDecision = 'execute' | 'supplement' | 'cancel';

export interface PlanState {
    lastCheck: CheckResult | undefined;
}
