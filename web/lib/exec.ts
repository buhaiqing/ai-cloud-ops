// Exec client (M3-4/M3-5): human-in-the-loop plan → approve → execute flow.
// Endpoint shapes follow audit-results/contract-m3-5.md; the existing
// GET /api/v1/analyses/{id} (internal/api/router.go) supplies the diagnosis.

import { getCSRF } from './auth';

// --- types ---
export type RiskLevel = 'low' | 'medium' | 'high' | 'irreversible';
export type ExecStatus =
  | 'planned'
  | 'approved'
  | 'running'
  | 'completed'
  | 'failed'
  | 'rolled_back';

export type Recommendation = {
  action: string;
  command: string;
  expected_outcome: string;
  // M3 extensions (contract-m3-agent.md). Optional until the parallel M3-2
  // backend backfills them into the analyses JSONB; UI renders defensively.
  preconditions?: string[];
  rollback_command?: string;
  risk_level?: RiskLevel;
  estimated_downtime_s?: number;
};

export type EvidenceChain = {
  claim: string;
  supporting_tool: string;
  supporting_data: string;
};

// Shape returned by GET /api/v1/analyses/{id} (backend echoes id as string).
export type Diagnosis = {
  id: number | string;
  model: string;
  prompt_version: string;
  root_cause: string;
  recommendations: Recommendation[];
  evidence: EvidenceChain[];
  latency_ms: number;
  created_at: string;
};

export type PlannedAction = {
  tool_name: string;
  command: string;
  target_resource: string;
  risk_level: string;
  rollback: string;
  preconditions_met: string[];
};

// POST /api/v1/exec/plan response_200
export type PlanResult = {
  plan_id: number;
  would_execute: PlannedAction[];
  blocked_by_policy: string[];
};

// POST /api/v1/exec/approve response_200
export type ApproveResult = { exec_id: number; status: string };

// POST /api/v1/exec/{exec_id}/execute response_200
export type ExecuteResult = { exec_id: number; status: ExecStatus; actions_total: number };

export type ExecAuditRow = {
  id: number;
  action_name: string;
  target_resource: string;
  status: string;
  error: string | null;
  started_at: string;
  completed_at: string | null;
};

// contract-m3-4.md frontend_types Execution. audit_trail is only present on
// GET /api/v1/exec/{id}; list rows omit it.
export type Execution = {
  id: number;
  plan_id: number;
  status: ExecStatus;
  actions_total: number;
  actions_completed: number;
  started_at: string;
  completed_at: string | null;
  audit_trail?: ExecAuditRow[];
};

// GET /api/v1/exec/{id} names the key exec_id (contract-m3-5.md).
type ExecutionResponse = Omit<Execution, 'id'> & { exec_id: number };

// --- fetch wrapper: api.ts pattern + CSRF on state-changing calls ---
async function execFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const method = (init?.method ?? 'GET').toUpperCase();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string> | undefined),
  };
  if (method !== 'GET') headers['X-CSRF-Token'] = getCSRF();
  const res = await fetch(url, { ...init, method, headers, cache: 'no-store' });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status} ${res.statusText}: ${text}`);
  }
  return res.json();
}

// --- endpoints ---
export function getAnalysis(id: number | string): Promise<Diagnosis> {
  return execFetch<Diagnosis>(`/api/v1/analyses/${id}`);
}

export function plan(diagnosisId: number | string, dryRun = true): Promise<PlanResult> {
  return execFetch<PlanResult>('/api/v1/exec/plan', {
    method: 'POST',
    body: JSON.stringify({ diagnosis_id: Number(diagnosisId), dry_run: dryRun }),
  });
}

export function approve(planId: number, note?: string): Promise<ApproveResult> {
  const body: { plan_id: number; approver_note?: string } = { plan_id: planId };
  if (note) body.approver_note = note;
  return execFetch<ApproveResult>('/api/v1/exec/approve', {
    method: 'POST',
    body: JSON.stringify(body),
  });
}

export function execute(execId: number | string): Promise<ExecuteResult> {
  return execFetch<ExecuteResult>(`/api/v1/exec/${execId}/execute`, { method: 'POST' });
}

export async function getExecution(execId: number | string): Promise<Execution> {
  const raw = await execFetch<ExecutionResponse>(`/api/v1/exec/${execId}`);
  const { exec_id, ...rest } = raw;
  return { ...rest, id: exec_id };
}

// Contract gap: contract-m3-5 defines no list endpoint; the /executions page
// needs one, so we assume GET /api/v1/executions[?account=].
export function listExecutions(account?: string): Promise<Execution[]> {
  const q = account ? `?account=${encodeURIComponent(account)}` : '';
  return execFetch<Execution[]>(`/api/v1/executions${q}`);
}
