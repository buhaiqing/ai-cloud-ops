'use client';
// M3-4: execution history for the current account.
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { listExecutions, Execution } from '../../lib/exec';

const STATUS_BG: Record<string, string> = {
  planned: 'bg-gray-500',
  approved: 'bg-blue-600',
  running: 'bg-amber-500',
  completed: 'bg-emerald-500',
  failed: 'bg-red-600',
  rolled_back: 'bg-purple-600',
};

export default function ExecutionsPage() {
  const [execs, setExecs] = useState<Execution[] | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    listExecutions().then(setExecs).catch((e) => setErr(String(e)));
  }, []);

  return (
    <section>
      <h1 className="text-2xl font-bold mb-4">Executions</h1>
      {err && <div className="text-red-500">{err}</div>}
      {!execs && !err && <div>Loading…</div>}
      {execs && execs.length === 0 && <div className="text-gray-500">No executions.</div>}
      {execs && execs.length > 0 && (
        <div className="table-wrap bg-white dark:bg-gray-900 rounded shadow">
          <table className="dash" data-testid="executions-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Status</th>
                <th>Plan</th>
                <th>Progress</th>
                <th>Started</th>
                <th>Completed</th>
              </tr>
            </thead>
            <tbody>
              {execs.map((e) => (
                <tr key={e.id} data-testid={`exec-row-${e.id}`}>
                  <td>
                    <Link href={`/executions/${e.id}`} className="text-blue-600 underline">
                      #{e.id}
                    </Link>
                  </td>
                  <td>
                    <span className={`severity-pill ${STATUS_BG[e.status] ?? 'bg-gray-500'}`}>
                      {e.status}
                    </span>
                  </td>
                  <td>{e.plan_id}</td>
                  <td>
                    {e.actions_completed}/{e.actions_total}
                  </td>
                  <td>{new Date(e.started_at).toLocaleString()}</td>
                  <td>{e.completed_at ? new Date(e.completed_at).toLocaleString() : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
