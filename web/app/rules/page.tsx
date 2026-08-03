'use client';
import { useEffect, useState, useCallback } from 'react';
import Link from 'next/link';
import { api, Rule } from '../../lib/api';
import RuleForm, { RuleFormData } from '../../components/RuleForm';

const SEV_BG: Record<string, string> = {
  critical: 'bg-red-600',
  warning: 'bg-amber-500',
  info: 'bg-blue-500',
};

export default function RulesPage() {
  const [rules, setRules] = useState<Rule[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const refresh = useCallback(() => {
    api.listRules()
      .then(setRules)
      .catch((e) => setErr(String(e)));
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleCreate = async (data: RuleFormData) => {
    try {
      await api.createRule(data as Partial<Rule>);
      setCreating(false);
      refresh();
    } catch (e) {
      setErr(String(e));
    }
  };

  const handleDelete = async (id: number) => {
    if (!window.confirm(`Delete rule ${id}?`)) return;
    try {
      await api.deleteRule(id);
      refresh();
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <section>
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-2xl font-bold">Rules</h1>
        {!creating && (
          <button
            data-testid="btn-new"
            onClick={() => setCreating(true)}
            className="px-3 py-1.5 text-sm rounded bg-blue-600 text-white"
          >
            + New rule
          </button>
        )}
      </div>
      {creating && (
        <RuleForm onSubmit={handleCreate} submitLabel="Create" />
      )}
      {err && <div className="text-red-500 mb-2">{err}</div>}
      {!rules && !err && <div>Loading…</div>}
      {rules && rules.length === 0 && (
        <div className="text-gray-500" data-testid="rules-empty">No rules yet.</div>
      )}
      {rules && rules.length > 0 && (
        <div className="table-wrap bg-white dark:bg-gray-900 rounded shadow">
          <table className="dash" data-testid="rules-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Account</th>
                <th>Name</th>
                <th>Severity</th>
                <th>Metric</th>
                <th>Threshold</th>
                <th>Enabled</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {rules.map((r) => (
                <tr key={r.id} data-testid="rule-row">
                  <td>{r.id}</td>
                  <td>{r.account_alias}</td>
                  <td>{r.name}</td>
                  <td>
                    <span className={`severity-pill ${SEV_BG[r.severity] || 'bg-gray-400'}`}>
                      {r.severity}
                    </span>
                  </td>
                  <td>{r.metric}</td>
                  <td>{r.threshold ?? '—'}</td>
                  <td>{r.enabled ? 'yes' : 'no'}</td>
                  <td className="flex gap-2">
                    <Link
                      href={`/rules/${r.id}`}
                      data-testid={`btn-edit-${r.id}`}
                      className="px-2 py-0.5 text-xs rounded bg-gray-200 dark:bg-gray-700 hover:bg-gray-300"
                    >
                      Edit
                    </Link>
                    <button
                      onClick={() => handleDelete(r.id)}
                      data-testid={`btn-delete-${r.id}`}
                      className="px-2 py-0.5 text-xs rounded bg-red-600 text-white hover:bg-red-700"
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}