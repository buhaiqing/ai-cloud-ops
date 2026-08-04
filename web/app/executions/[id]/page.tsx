'use client';
// M3-4: execution progress + audit trail. Polls every 2s until the
// execution reaches a terminal state (completed/failed/rolled_back).
import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'next/navigation';
import { getExecution, Execution, ExecAuditRow } from '../../../lib/exec';

const POLL_MS = 2000;
const TERMINAL = new Set(['completed', 'failed', 'rolled_back']);

function ExecutionStatus({ exec }: { exec: Execution }) {
  return (
    <div className="mb-4">
      <h1 className="text-2xl font-bold">Execution #{exec.id}</h1>
      <div data-testid="exec-status" className="text-lg font-semibold">
        {exec.status}
      </div>
      <div className="text-sm text-gray-500">
        plan {exec.plan_id} · started {new Date(exec.started_at).toLocaleString()}
        {exec.completed_at && ` · completed ${new Date(exec.completed_at).toLocaleString()}`}
      </div>
    </div>
  );
}

function ActionList({ rows }: { rows: ExecAuditRow[] }) {
  return (
    <div className="mb-6">
      <h2 className="text-lg font-semibold mb-2">Actions</h2>
      {rows.map((a, idx) => (
        <div
          key={idx}
          data-testid={`action-row-${idx}`}
          className="flex items-center gap-2 p-2 border-b border-gray-200 dark:border-gray-800 text-sm"
        >
          <span className="font-medium">{a.action_name}</span>
          <span className="text-gray-500">→ {a.target_resource}</span>
          <span className="ml-auto font-semibold">{a.status}</span>
        </div>
      ))}
    </div>
  );
}

function AuditTrail({ rows }: { rows: ExecAuditRow[] }) {
  return (
    <div>
      <h2 className="text-lg font-semibold mb-2">Audit trail</h2>
      {rows.map((a, idx) => (
        <div
          key={idx}
          data-testid={`audit-row-${idx}`}
          className="p-2 border-b border-gray-200 dark:border-gray-800 text-xs text-gray-600 dark:text-gray-400"
        >
          <span className="font-medium text-gray-800 dark:text-gray-200">
            {a.action_name}
          </span>{' '}
          on {a.target_resource}: {a.started_at}
          {a.completed_at ? ` → ${a.completed_at}` : ''} · {a.status}
          {a.error && <span className="text-red-500"> · error: {a.error}</span>}
        </div>
      ))}
    </div>
  );
}

export default function ExecutionDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id;
  const [exec, setExec] = useState<Execution | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const refresh = useCallback(() => {
    if (!id) return;
    getExecution(id).then(setExec).catch((e) => setErr(String(e)));
  }, [id]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // Poll while non-terminal.
  const status = exec?.status;
  useEffect(() => {
    if (!status || TERMINAL.has(status)) return;
    const t = setInterval(refresh, POLL_MS);
    return () => clearInterval(t);
  }, [status, refresh]);

  if (err) return <div className="text-red-500">{err}</div>;
  if (!exec) return <div>Loading…</div>;

  const pct =
    exec.actions_total > 0
      ? Math.round((exec.actions_completed / exec.actions_total) * 100)
      : 0;
  const rows = exec.audit_trail ?? [];

  return (
    <section>
      <ExecutionStatus exec={exec} />

      <div className="mb-6">
        <div className="h-3 w-full bg-gray-200 dark:bg-gray-800 rounded overflow-hidden">
          <div
            data-testid="exec-progress-bar"
            className="h-full bg-blue-600 transition-all"
            style={{ width: `${pct}%` }}
          />
        </div>
        <div className="text-sm text-gray-500 mt-1">
          {exec.actions_completed}/{exec.actions_total} actions ({pct}%)
        </div>
      </div>

      <ActionList rows={rows} />
      <AuditTrail rows={rows} />
    </section>
  );
}
