// M3-4 tests: exec lib client + HITL approval UI (analyses page,
// executions list, execution detail). Fixtures follow contract-m3-5.md
// response_200 shapes exactly.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { plan, approve, execute, getExecution, listExecutions } from '../lib/exec';
import AnalysisPage from '../app/analyses/[id]/page';
import ExecutionsPage from '../app/executions/page';
import ExecutionDetailPage from '../app/executions/[id]/page';

function mockJsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const useParamsMock = vi.fn(() => ({ id: '7' }));
const pushMock = vi.fn();
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock, refresh: vi.fn() }),
  useParams: () => useParamsMock(),
}));

// --- fixtures (contract-m3-5.md response_200) ---
const DIAGNOSIS = {
  id: 7,
  model: 'claude-sonnet-4',
  prompt_version: 'v3',
  root_cause: 'rds_cpu_saturation',
  recommendations: [
    {
      action: 'Restart RDS instance',
      command: 'restart_rds_instance rm-abc',
      expected_outcome: 'CPU back below 60%',
      preconditions: ['no active failover', 'backup within 24h'],
      rollback_command: 'n/a (transient restart)',
      risk_level: 'medium',
      estimated_downtime_s: 120,
    },
    {
      action: 'Delete stale snapshots',
      command: 'delete_snapshots --older-than 30d',
      expected_outcome: 'Free 40GB',
      preconditions: [],
      rollback_command: '',
      risk_level: 'irreversible',
      estimated_downtime_s: 0,
    },
  ],
  evidence: [
    { claim: 'CPU at 98%', supporting_tool: 'describe_rds', supporting_data: 'cpu=98' },
  ],
  latency_ms: 4200,
  created_at: '2026-08-03T10:00:00Z',
};

const PLAN_RESULT = {
  plan_id: 11,
  would_execute: [
    {
      tool_name: 'restart_rds_instance',
      command: 'RestartDBInstance rm-abc',
      target_resource: 'rm-abc',
      risk_level: 'medium',
      rollback: 'n/a',
      preconditions_met: ['no active failover: ok'],
    },
  ],
  blocked_by_policy: ['delete_snapshots not in WRITE_TOOLS whitelist'],
};

const APPROVE_RESULT = { exec_id: 21, status: 'approved' };
const EXECUTE_RESULT = { exec_id: 21, status: 'running', actions_total: 2 };

const EXEC_DETAIL_RUNNING = {
  exec_id: 21,
  plan_id: 11,
  status: 'running',
  actions_total: 2,
  actions_completed: 1,
  started_at: '2026-08-03T11:00:00Z',
  completed_at: null,
  audit_trail: [
    {
      id: 1,
      action_name: 'restart_rds_instance',
      target_resource: 'rm-abc',
      status: 'completed',
      error: null,
      started_at: '2026-08-03T11:00:00Z',
      completed_at: '2026-08-03T11:02:00Z',
    },
    {
      id: 2,
      action_name: 'remove_ecs_from_slb',
      target_resource: 'i-xyz',
      status: 'running',
      error: null,
      started_at: '2026-08-03T11:02:00Z',
      completed_at: null,
    },
  ],
};

const EXEC_LIST = [
  {
    id: 21,
    plan_id: 11,
    status: 'completed',
    actions_total: 2,
    actions_completed: 2,
    started_at: '2026-08-03T11:00:00Z',
    completed_at: '2026-08-03T11:05:00Z',
  },
  {
    id: 22,
    plan_id: 12,
    status: 'failed',
    actions_total: 1,
    actions_completed: 0,
    started_at: '2026-08-03T12:00:00Z',
    completed_at: '2026-08-03T12:01:00Z',
  },
];

// Route-aware fetch mock for page tests.
function routeFetch(url: unknown, init?: RequestInit): Promise<Response> {
  const u = String(url);
  if (u.includes('/api/v1/analyses/7')) return Promise.resolve(mockJsonResponse(DIAGNOSIS));
  if (u.endsWith('/api/v1/exec/plan')) return Promise.resolve(mockJsonResponse(PLAN_RESULT));
  if (u.endsWith('/api/v1/exec/approve')) return Promise.resolve(mockJsonResponse(APPROVE_RESULT));
  if (u.includes('/api/v1/exec/21/execute')) return Promise.resolve(mockJsonResponse(EXECUTE_RESULT));
  if (u.includes('/api/v1/exec/21')) return Promise.resolve(mockJsonResponse(EXEC_DETAIL_RUNNING));
  if (u.includes('/api/v1/executions')) return Promise.resolve(mockJsonResponse(EXEC_LIST));
  return Promise.resolve(mockJsonResponse({}));
}

beforeEach(() => {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).fetch = vi.fn(() => Promise.resolve(mockJsonResponse({})));
  useParamsMock.mockReset();
  useParamsMock.mockImplementation(() => ({ id: '7' }));
  pushMock.mockReset();
  document.cookie = '';
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.useRealTimers();
});

// --- lib/exec.ts ---
describe('exec lib', () => {
  it('plan() POSTs diagnosis_id + dry_run and reads CSRF cookie at call time', async () => {
    const fetchMock = vi.fn((..._args: unknown[]) => Promise.resolve(mockJsonResponse(PLAN_RESULT)));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    document.cookie = 'aico_csrf=token-A';
    await plan(7);
    document.cookie = 'aico_csrf=token-B';
    await plan(7, false);

    expect(fetchMock).toHaveBeenCalledTimes(2);
    const [url1, init1] = fetchMock.mock.calls[0];
    expect(url1).toBe('/api/v1/exec/plan');
    expect((init1 as RequestInit).method).toBe('POST');
    expect(JSON.parse((init1 as RequestInit).body as string)).toEqual({
      diagnosis_id: 7,
      dry_run: true,
    });
    expect((init1 as RequestInit).headers).toMatchObject({ 'X-CSRF-Token': 'token-A' });
    const init2 = fetchMock.mock.calls[1][1] as RequestInit;
    expect(JSON.parse(init2.body as string)).toEqual({ diagnosis_id: 7, dry_run: false });
    expect(init2.headers).toMatchObject({ 'X-CSRF-Token': 'token-B' });
  });

  it('approve() sends plan_id + note; execute() POSTs no body', async () => {
    const fetchMock = vi.fn((url: unknown, init?: RequestInit) => {
      if (String(url).includes('/approve')) return Promise.resolve(mockJsonResponse(APPROVE_RESULT));
      return Promise.resolve(mockJsonResponse(EXECUTE_RESULT));
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;

    const ap = await approve(11, 'verified in dry-run');
    expect(ap).toEqual(APPROVE_RESULT);
    expect(JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string)).toEqual({
      plan_id: 11,
      approver_note: 'verified in dry-run',
    });

    const ex = await execute(21);
    expect(ex).toEqual(EXECUTE_RESULT);
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/exec/21/execute');
    expect((fetchMock.mock.calls[1][1] as RequestInit).method).toBe('POST');
  });

  it('getExecution() normalizes exec_id to id', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(() => Promise.resolve(mockJsonResponse(EXEC_DETAIL_RUNNING)));
    const exec = await getExecution(21);
    expect(exec.id).toBe(21);
    expect(exec.status).toBe('running');
    expect(exec.audit_trail).toHaveLength(2);
  });

  it('listExecutions() appends the account filter', async () => {
    const fetchMock = vi.fn((..._args: unknown[]) => Promise.resolve(mockJsonResponse(EXEC_LIST)));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    await listExecutions();
    await listExecutions('prod');
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/executions');
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/executions?account=prod');
  });
});

// --- /analyses/[id] ---
describe('AnalysisPage', () => {
  it('shows error state when analysis fetch fails', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(() =>
      Promise.resolve(new Response('boom', { status: 500, statusText: 'Internal Server Error' })),
    );
    render(<AnalysisPage />);
    await waitFor(() => expect(screen.getByText(/500/)).toBeInTheDocument());
  });

  it('renders diagnosis header and recommendation cards with all fields', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(routeFetch);
    render(<AnalysisPage />);
    await waitFor(() => expect(screen.getByTestId('analysis-root')).toBeInTheDocument());

    // DiagnosisHeader
    expect(screen.getByText('rds_cpu_saturation')).toBeInTheDocument();
    expect(screen.getByText(/claude-sonnet-4/)).toBeInTheDocument();

    // Recommendation cards
    const cards = [screen.getByTestId('rec-card-0'), screen.getByTestId('rec-card-1')];
    expect(cards[0]).toHaveTextContent('Restart RDS instance');
    expect(cards[0]).toHaveTextContent('CPU back below 60%');
    expect(cards[0]).toHaveTextContent('no active failover');
    expect(cards[0]).toHaveTextContent('backup within 24h');
    expect(cards[0]).toHaveTextContent('120');
    expect(screen.getByTestId('rollback-code-0')).toHaveTextContent('n/a (transient restart)');

    // Risk pills with contract colors: medium=amber, irreversible=purple
    expect(screen.getByTestId('risk-pill-0')).toHaveTextContent('medium');
    expect(screen.getByTestId('risk-pill-0').className).toContain('bg-amber-500');
    expect(screen.getByTestId('risk-pill-1')).toHaveTextContent('irreversible');
    expect(screen.getByTestId('risk-pill-1').className).toContain('bg-purple-600');
  });

  it('DryRun calls /plan with dry_run=true, shows summary, unlocks Approve', async () => {
    const fetchMock = vi.fn(routeFetch);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    render(<AnalysisPage />);
    await waitFor(() => expect(screen.getByTestId('btn-dry-run')).toBeInTheDocument());

    expect(screen.getByTestId('btn-approve')).toBeDisabled();
    fireEvent.click(screen.getByTestId('btn-dry-run'));

    await waitFor(() => {
      const post = fetchMock.mock.calls.find((c) => String(c[0]).endsWith('/api/v1/exec/plan'));
      expect(post).toBeDefined();
      expect(JSON.parse((post![1] as RequestInit).body as string)).toEqual({
        diagnosis_id: 7,
        dry_run: true,
      });
    });
    await waitFor(() => expect(screen.getByTestId('dry-run-summary')).toBeInTheDocument());
    expect(screen.getByTestId('dry-run-summary')).toHaveTextContent('restart_rds_instance');
    expect(screen.getByTestId('dry-run-summary')).toHaveTextContent(
      'delete_snapshots not in WRITE_TOOLS whitelist',
    );
    expect(screen.getByTestId('btn-approve')).toBeEnabled();
  });

  it('shows error when dry-run plan fails', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn((url: unknown) => {
      if (String(url).endsWith('/api/v1/exec/plan')) {
        return Promise.resolve(
          new Response('rate limited', { status: 429, statusText: 'Too Many Requests' }),
        );
      }
      return routeFetch(url);
    });
    render(<AnalysisPage />);
    await waitFor(() => expect(screen.getByTestId('btn-dry-run')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('btn-dry-run'));
    await waitFor(() => expect(screen.getByText(/429/)).toBeInTheDocument());
    expect(screen.getByTestId('btn-approve')).toBeDisabled();
  });

  it('approve happy path: approve → execute → navigate to /executions/{id}', async () => {
    const fetchMock = vi.fn(routeFetch);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    render(<AnalysisPage />);
    await waitFor(() => expect(screen.getByTestId('btn-dry-run')).toBeInTheDocument());

    fireEvent.click(screen.getByTestId('btn-dry-run'));
    await waitFor(() => expect(screen.getByTestId('btn-approve')).toBeEnabled());
    fireEvent.click(screen.getByTestId('btn-approve'));

    await waitFor(() => {
      const post = fetchMock.mock.calls.find((c) => String(c[0]).endsWith('/api/v1/exec/approve'));
      expect(post).toBeDefined();
      expect(JSON.parse((post![1] as RequestInit).body as string)).toEqual({ plan_id: 11 });
    });

    await waitFor(() => expect(screen.getByTestId('btn-execute')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('btn-execute'));

    await waitFor(() => {
      const post = fetchMock.mock.calls.find((c) =>
        String(c[0]).includes('/api/v1/exec/21/execute'),
      );
      expect(post).toBeDefined();
    });
    await waitFor(() => expect(pushMock).toHaveBeenCalledWith('/executions/21'));
  });

  it('modify toggles the approver note and sends it with approve', async () => {
    const fetchMock = vi.fn(routeFetch);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    render(<AnalysisPage />);
    await waitFor(() => expect(screen.getByTestId('btn-dry-run')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('btn-dry-run'));
    await waitFor(() => expect(screen.getByTestId('btn-approve')).toBeEnabled());

    expect(screen.queryByTestId('approve-note')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('btn-modify'));
    fireEvent.change(screen.getByTestId('approve-note'), {
      target: { value: 'checked with on-call' },
    });
    fireEvent.click(screen.getByTestId('btn-approve'));

    await waitFor(() => {
      const post = fetchMock.mock.calls.find((c) => String(c[0]).endsWith('/api/v1/exec/approve'));
      expect(JSON.parse((post![1] as RequestInit).body as string)).toEqual({
        plan_id: 11,
        approver_note: 'checked with on-call',
      });
    });
  });

  it('reject opens modal; confirm rejects locally without backend call', async () => {
    const fetchMock = vi.fn(routeFetch);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    render(<AnalysisPage />);
    await waitFor(() => expect(screen.getByTestId('btn-reject')).toBeInTheDocument());

    fireEvent.click(screen.getByTestId('btn-reject'));
    await waitFor(() => expect(screen.getByTestId('reject-modal')).toBeInTheDocument());
    fireEvent.change(screen.getByTestId('reject-reason'), {
      target: { value: 'maintenance window closed' },
    });
    fireEvent.click(screen.getByTestId('btn-reject-confirm'));

    await waitFor(() => expect(screen.getByText(/rejected/i)).toBeInTheDocument());
    // Only the analysis GET happened — no approve/reject endpoint exists (contract gap).
    for (const call of fetchMock.mock.calls) {
      expect(String(call[0])).not.toContain('/api/v1/exec/');
    }
    expect(screen.queryByTestId('btn-approve')).not.toBeInTheDocument();
  });
});

// --- /executions ---
describe('ExecutionsPage', () => {
  it('renders loading, then table rows with exec-row-{id}', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(routeFetch);
    render(<ExecutionsPage />);
    expect(screen.getByText(/loading/i)).toBeInTheDocument();
    await waitFor(() => expect(screen.getByTestId('executions-table')).toBeInTheDocument());
    expect(screen.getByTestId('exec-row-21')).toHaveTextContent('completed');
    expect(screen.getByTestId('exec-row-22')).toHaveTextContent('failed');
  });

  it('shows empty state', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(() => Promise.resolve(mockJsonResponse([])));
    render(<ExecutionsPage />);
    await waitFor(() => expect(screen.getByText(/no executions/i)).toBeInTheDocument());
  });

  it('shows error state', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(() =>
      Promise.resolve(new Response('down', { status: 503, statusText: 'Service Unavailable' })),
    );
    render(<ExecutionsPage />);
    await waitFor(() => expect(screen.getByText(/503/)).toBeInTheDocument());
  });
});

// --- /executions/[id] ---
describe('ExecutionDetailPage', () => {
  it('renders status, progress bar, action rows and audit rows', async () => {
    useParamsMock.mockImplementation(() => ({ id: '21' }));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(routeFetch);
    render(<ExecutionDetailPage />);

    await waitFor(() => expect(screen.getByTestId('exec-status')).toBeInTheDocument());
    expect(screen.getByTestId('exec-status')).toHaveTextContent('running');
    // 1 of 2 completed → 50%
    expect(screen.getByTestId('exec-progress-bar').getAttribute('style')).toContain('width: 50%');
    expect(screen.getByTestId('action-row-0')).toHaveTextContent('restart_rds_instance');
    expect(screen.getByTestId('action-row-1')).toHaveTextContent('running');
    expect(screen.getByTestId('audit-row-0')).toHaveTextContent('2026-08-03T11:00:00Z');
    expect(screen.getByTestId('audit-row-1')).toHaveTextContent('i-xyz');
  });

  it('polls while running and stops at terminal status', async () => {
    useParamsMock.mockImplementation(() => ({ id: '21' }));
    vi.useFakeTimers();
    let calls = 0;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(() => {
      calls++;
      const body = calls === 1 ? EXEC_DETAIL_RUNNING : { ...EXEC_DETAIL_RUNNING, status: 'completed', actions_completed: 2, completed_at: '2026-08-03T11:05:00Z' };
      return Promise.resolve(mockJsonResponse(body));
    });

    render(<ExecutionDetailPage />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByTestId('exec-status')).toHaveTextContent('running');
    expect(calls).toBe(1);

    // One poll interval (2s) later the refreshed status renders.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });
    expect(screen.getByTestId('exec-status')).toHaveTextContent('completed');
    expect(calls).toBe(2);

    // Terminal status → polling stops.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(calls).toBe(2);
  });
});