'use client';
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { api, Alert } from '../../lib/api';

const SEV_BG: Record<string, string> = {
  critical: 'bg-red-600',
  warning: 'bg-amber-500',
  info: 'bg-blue-500',
};
const STATUS_BG: Record<string, string> = {
  open: 'bg-red-600',
  acknowledged: 'bg-amber-500',
  suppressed: 'bg-gray-500',
  maintenance: 'bg-violet-500',
  resolved: 'bg-emerald-500',
};

export default function AlertsPage() {
  const [alerts, setAlerts] = useState<Alert[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [statusFilter, setStatusFilter] = useState<string>('');

  useEffect(() => {
    api.listAlerts({ status: statusFilter || undefined, limit: 100 })
      .then(setAlerts)
      .catch((e) => setErr(String(e)));
  }, [statusFilter]);

  return (
    <section>
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-bold">Alerts</h1>
        <select
          aria-label="Filter by status"
          className="border rounded px-2 py-1 text-sm bg-white dark:bg-gray-900"
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
        >
          <option value="">All</option>
          <option value="open">Open</option>
          <option value="acknowledged">Acknowledged</option>
          <option value="suppressed">Suppressed</option>
          <option value="maintenance">Maintenance</option>
          <option value="resolved">Resolved</option>
        </select>
      </div>
      {err && <div className="text-red-500 mb-2">{err}</div>}
      {!alerts && !err && <div>Loading…</div>}
      {alerts && alerts.length === 0 && <div className="text-gray-500">No alerts.</div>}
      {alerts && alerts.length > 0 && (
        <div className="table-wrap bg-white dark:bg-gray-900 rounded shadow">
          <table className="dash" data-testid="alerts-table">
            <thead>
              <tr>
                <th>Severity</th>
                <th>Status</th>
                <th>Name</th>
                <th>Account</th>
                <th>Region</th>
                <th>Created</th>
              </tr>
            </thead>
            <tbody>
              {alerts.map((a) => (
                <tr key={a.id} data-testid="alert-row">
                  <td>
                    <span className={`severity-pill ${SEV_BG[a.severity] || 'bg-gray-400'}`}>
                      {a.severity}
                    </span>
                  </td>
                  <td>
                    <span className={`severity-pill ${STATUS_BG[a.status] || 'bg-gray-400'}`}>
                      {a.status}
                    </span>
                  </td>
                  <td>
                    <Link href={`/alerts/${a.id}`} className="text-blue-600 hover:underline">
                      {a.name || a.alert_id}
                    </Link>
                  </td>
                  <td>{a.account}</td>
                  <td>{a.region}</td>
                  <td className="text-gray-500">{new Date(a.created_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}