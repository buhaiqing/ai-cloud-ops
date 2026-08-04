'use client';
import { useEffect, useState, useCallback } from 'react';
import Link from 'next/link';
import { useParams } from 'next/navigation';
import { api } from '../../../lib/api';

type Analysis = { id: number; model: string; root_cause: string; latency_ms: number; created_at: string };
type Detail = {
  id: number;
  alert_id: string;
  account: string;
  region: string;
  severity: string;
  status: string;
  name: string;
  created_at: string;
  analyses: Analysis[];
};

const NEXT: Record<string, string> = {
  open: 'ack',
  acknowledged: 'suppress|maintenance|resolve',
  suppressed: 'replay',
  maintenance: 'replay',
  resolved: 'replay',
};

export default function AlertDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id;
  const [alert, setAlert] = useState<Detail | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    if (!id) return;
    try {
      const data = (await api.getAlert(id)) as unknown as Detail;
      setAlert(data);
    } catch (e) {
      setErr(String(e));
    }
  }, [id]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const transition = async (op: 'ack' | 'suppress' | 'maintenance' | 'resolve' | 'replay') => {
    setBusy(true);
    setErr(null);
    try {
      const fn = {
        ack: api.ackIncident,
        suppress: api.suppressIncident,
        maintenance: api.maintenanceIncident,
        resolve: api.resolveIncident,
        replay: api.replayIncident,
      }[op];
      await fn(Number(id));
      await refresh();
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  };

  if (err) return <div className="text-red-500">{err}</div>;
  if (!alert) return <div>Loading…</div>;

  const allowed = (NEXT[alert.status] || '').split('|').filter(Boolean);

  return (
    <section>
      <div className="mb-4">
        <h1 className="text-2xl font-bold">{alert.name || alert.alert_id}</h1>
        <div className="text-sm text-gray-500">
          {alert.account} / {alert.region} · severity: {alert.severity} · status: {alert.status}
        </div>
      </div>

      <div className="flex gap-2 mb-6" data-testid="action-row">
        {allowed.map((op) => (
          <button
            key={op}
            disabled={busy}
            onClick={() => transition(op as 'ack' | 'suppress' | 'maintenance' | 'resolve' | 'replay')}
            className="px-3 py-1.5 text-sm rounded bg-blue-600 text-white disabled:opacity-50"
            data-testid={`btn-${op}`}
          >
            {op}
          </button>
        ))}
        {allowed.length === 0 && <span className="text-gray-500 text-sm">No further actions.</span>}
      </div>

      <h2 className="text-lg font-semibold mb-2">AI Analyses</h2>
      {alert.analyses.length === 0 && <div className="text-gray-500">No analyses yet.</div>}
      {alert.analyses.map((a) => (
        <div key={a.id} className="mb-4 p-4 bg-white dark:bg-gray-900 rounded shadow">
          <div className="text-xs text-gray-500 mb-1">
            {a.model} · {a.latency_ms}ms · {new Date(a.created_at).toLocaleString()}
          </div>
          <div className="font-medium">{a.root_cause}</div>
          <Link
            href={`/analyses/${a.id}`}
            data-testid={`analysis-link-${a.id}`}
            className="text-sm text-blue-600 underline"
          >
            View diagnosis →
          </Link>
        </div>
      ))}
    </section>
  );
}