'use client';
// M3-4: AI diagnosis report + human-in-the-loop approval flow
// (dry-run → approve → execute). Contract: audit-results/contract-m3-4.md.
import { useCallback, useEffect, useState } from 'react';
import Link from 'next/link';
import { useParams, useRouter } from 'next/navigation';
import {
  approve,
  execute,
  getAnalysis,
  plan,
  Diagnosis,
  PlanResult,
  PlannedAction,
  Recommendation,
} from '../../../lib/exec';

const RISK_BG: Record<string, string> = {
  low: 'bg-green-600',
  medium: 'bg-amber-500',
  high: 'bg-red-600',
  irreversible: 'bg-purple-600',
};

function RiskPill({ level, testid }: { level: string; testid: string }) {
  return (
    <span
      data-testid={testid}
      className={`severity-pill ${RISK_BG[level] ?? 'bg-gray-500'}`}
    >
      {level}
    </span>
  );
}

function DiagnosisHeader({ d }: { d: Diagnosis }) {
  return (
    <div className="mb-4 p-4 bg-white dark:bg-gray-900 rounded shadow">
      <h1 className="text-2xl font-bold mb-1">{d.root_cause}</h1>
      <div className="text-sm text-gray-500">
        {d.model} · prompt {d.prompt_version} · {d.latency_ms}ms ·{' '}
        {new Date(d.created_at).toLocaleString()}
      </div>
      {d.evidence.length > 0 && (
        <ul className="mt-2 text-sm text-gray-600 dark:text-gray-400 list-disc pl-5">
          {d.evidence.map((e, i) => (
            <li key={i}>
              {e.claim} <span className="text-gray-400">({e.supporting_tool}: {e.supporting_data})</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function RecommendationCard({ rec, idx }: { rec: Recommendation; idx: number }) {
  return (
    <div
      data-testid={`rec-card-${idx}`}
      className="mb-4 p-4 bg-white dark:bg-gray-900 rounded shadow"
    >
      <div className="flex items-center gap-2 mb-2">
        <RiskPill level={rec.risk_level ?? 'unknown'} testid={`risk-pill-${idx}`} />
        <span className="font-semibold">{rec.action}</span>
      </div>
      <pre className="text-xs bg-gray-100 dark:bg-gray-800 rounded p-2 overflow-x-auto">{rec.command}</pre>
      <div className="text-sm mt-2">Expected: {rec.expected_outcome}</div>
      <div className="text-sm">
        Est. downtime: {rec.estimated_downtime_s ? `${rec.estimated_downtime_s}s` : 'none'}
      </div>
      {(rec.preconditions ?? []).length > 0 && (
        <div className="mt-2">
          <div className="text-xs font-semibold text-gray-500 uppercase">Preconditions</div>
          <ul className="list-disc pl-5 text-sm">
            {(rec.preconditions ?? []).map((p, i) => (
              <li key={i}>{p}</li>
            ))}
          </ul>
        </div>
      )}
      <div className="mt-2">
        <div className="text-xs font-semibold text-gray-500 uppercase">Rollback</div>
        <code
          data-testid={`rollback-code-${idx}`}
          className="text-xs bg-gray-100 dark:bg-gray-800 rounded px-1"
        >
          {rec.rollback_command || 'none (irreversible)'}
        </code>
      </div>
    </div>
  );
}

function DryRunSummary({ result }: { result: PlanResult }) {
  return (
    <div
      data-testid="dry-run-summary"
      className="mb-4 p-4 border border-blue-300 dark:border-blue-700 rounded bg-blue-50 dark:bg-blue-950"
    >
      <h2 className="font-semibold mb-2">Dry-run plan #{result.plan_id}</h2>
      {result.would_execute.length === 0 && (
        <div className="text-sm text-gray-500">No actions would execute.</div>
      )}
      {result.would_execute.map((a: PlannedAction, i: number) => (
        <div key={i} className="text-sm mb-1">
          ▸ {a.tool_name} → {a.target_resource} <span className="text-gray-500">({a.command})</span>
        </div>
      ))}
      {result.blocked_by_policy.length > 0 && (
        <div className="mt-2">
          <div className="text-xs font-semibold text-red-600 uppercase">Blocked by policy</div>
          <ul className="list-disc pl-5 text-sm text-red-600">
            {result.blocked_by_policy.map((b, i) => (
              <li key={i}>{b}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

export default function AnalysisPage() {
  const params = useParams<{ id: string }>();
  const router = useRouter();
  const id = params?.id;

  const [diagnosis, setDiagnosis] = useState<Diagnosis | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const [planResult, setPlanResult] = useState<PlanResult | null>(null);
  const [execId, setExecId] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionErr, setActionErr] = useState<string | null>(null);

  // Modify = attach an approver note (no edit endpoint in the contract).
  const [noteOpen, setNoteOpen] = useState(false);
  const [note, setNote] = useState('');

  // Reject = local terminal state; contract-m3-5 defines no reject endpoint.
  const [rejectOpen, setRejectOpen] = useState(false);
  const [rejectReason, setRejectReason] = useState('');
  const [rejected, setRejected] = useState(false);

  useEffect(() => {
    if (!id) return;
    getAnalysis(id).then(setDiagnosis).catch((e) => setErr(String(e)));
  }, [id]);

  const runDryRun = useCallback(async () => {
    if (!id) return;
    setBusy(true);
    setActionErr(null);
    try {
      setPlanResult(await plan(id, true));
    } catch (e) {
      setActionErr(String(e));
    } finally {
      setBusy(false);
    }
  }, [id]);

  const runApprove = useCallback(async () => {
    if (!planResult) return;
    setBusy(true);
    setActionErr(null);
    try {
      const res = await approve(planResult.plan_id, note.trim() || undefined);
      setExecId(res.exec_id);
    } catch (e) {
      setActionErr(String(e));
    } finally {
      setBusy(false);
    }
  }, [planResult, note]);

  const runExecute = useCallback(async () => {
    if (execId === null) return;
    setBusy(true);
    setActionErr(null);
    try {
      await execute(execId);
      router.push(`/executions/${execId}`);
    } catch (e) {
      setActionErr(String(e));
      setBusy(false);
    }
  }, [execId, router]);

  if (err) return <div className="text-red-500">{err}</div>;
  if (!diagnosis) return <div>Loading…</div>;

  return (
    <section data-testid="analysis-root">
      <DiagnosisHeader d={diagnosis} />

      {diagnosis.recommendations.map((rec, idx) => (
        <RecommendationCard key={idx} rec={rec} idx={idx} />
      ))}
      {diagnosis.recommendations.length === 0 && (
        <div className="text-gray-500 mb-4">No recommendations.</div>
      )}

      {rejected ? (
        <div className="p-4 rounded bg-red-50 dark:bg-red-950 text-red-700 dark:text-red-300">
          Plan rejected: {rejectReason || 'no reason given'}
        </div>
      ) : (
        <div className="flex flex-wrap items-center gap-2 mb-4" data-testid="approve-bar">
          <button
            data-testid="btn-dry-run"
            disabled={busy}
            onClick={runDryRun}
            className="px-3 py-1.5 text-sm rounded bg-blue-600 text-white disabled:opacity-50"
          >
            Dry run
          </button>
          <button
            data-testid="btn-approve"
            disabled={busy || !planResult || execId !== null}
            onClick={runApprove}
            className="px-3 py-1.5 text-sm rounded bg-green-600 text-white disabled:opacity-50"
          >
            Approve
          </button>
          <button
            data-testid="btn-modify"
            onClick={() => setNoteOpen(!noteOpen)}
            className="px-3 py-1.5 text-sm rounded border border-gray-300 dark:border-gray-700"
          >
            Modify
          </button>
          <button
            data-testid="btn-reject"
            onClick={() => setRejectOpen(true)}
            className="px-3 py-1.5 text-sm rounded bg-red-600 text-white"
          >
            Reject
          </button>
          {execId !== null && (
            <button
              data-testid="btn-execute"
              disabled={busy}
              onClick={runExecute}
              className="px-3 py-1.5 text-sm rounded bg-purple-600 text-white disabled:opacity-50"
            >
              Execute
            </button>
          )}
          {execId !== null && (
            <Link href={`/executions/${execId}`} className="text-sm text-blue-600 underline">
              view execution
            </Link>
          )}
        </div>
      )}

      {noteOpen && (
        <textarea
          data-testid="approve-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="Approver note (sent with approval)"
          className="w-full mb-4 border rounded p-2 text-sm bg-white dark:bg-gray-900"
        />
      )}

      {actionErr && <div className="text-red-500 mb-4">{actionErr}</div>}

      {planResult && <DryRunSummary result={planResult} />}

      {rejectOpen && (
        <div
          data-testid="reject-modal"
          className="fixed inset-0 flex items-center justify-center bg-black/50"
        >
          <div className="bg-white dark:bg-gray-900 rounded shadow p-4 w-96">
            <h2 className="font-semibold mb-2">Reject plan</h2>
            <textarea
              data-testid="reject-reason"
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
              placeholder="Reason for rejection"
              className="w-full border rounded p-2 text-sm bg-white dark:bg-gray-900"
            />
            <div className="flex gap-2 mt-3 justify-end">
              <button
                onClick={() => setRejectOpen(false)}
                className="px-3 py-1.5 text-sm rounded border border-gray-300 dark:border-gray-700"
              >
                Cancel
              </button>
              <button
                data-testid="btn-reject-confirm"
                onClick={() => {
                  setRejectOpen(false);
                  setRejected(true);
                }}
                className="px-3 py-1.5 text-sm rounded bg-red-600 text-white"
              >
                Confirm reject
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
