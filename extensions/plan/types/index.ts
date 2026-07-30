export interface PlanTodo {
    title: string;
    goal: string;
    steps: string[];
    risks?: string[];
    acceptance: string[];
}

export interface CheckResult {
    status: 'pass' | 'fail';
    reason?: string;
    improvements?: string[];
}

export type PlanDecision = 'execute' | 'supplement' | 'cancel';

export interface PlanState {
    todos: PlanTodo[];
    lastCheck: CheckResult | undefined;
}
