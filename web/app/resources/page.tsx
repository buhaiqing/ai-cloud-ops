'use client';
import { useEffect, useState } from 'react';
import { api, Resource } from '../../lib/api';

// Common option lists — kept inline because this is a static filter UI, not
// a config UI. The page takes any string the user picks; backend ignores
// unknowns. (Avoids an extra /api/v1/accounts fetch on every page load.)
const ACCOUNT_OPTIONS = ['prod', 'staging', 'dev', 'sandbox'];
const REGION_OPTIONS = [
  'cn-hangzhou',
  'cn-shanghai',
  'cn-beijing',
  'cn-shenzhen',
  'cn-qingdao',
];
const TYPE_OPTIONS = ['ecs', 'rds', 'slb', 'oss', 'vpc'];

export default function ResourcesPage() {
  const [resources, setResources] = useState<Resource[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [accountFilter, setAccountFilter] = useState<string>('');
  const [regionFilter, setRegionFilter] = useState<string>('');
  const [typeFilter, setTypeFilter] = useState<string>('');

  useEffect(() => {
    setResources(null);
    setErr(null);
    api
      .listResources({
        account: accountFilter || undefined,
        region: regionFilter || undefined,
        type: typeFilter || undefined,
      })
      .then(setResources)
      .catch((e) => setErr(String(e)));
  }, [accountFilter, regionFilter, typeFilter]);

  return (
    <section>
      <div className="flex items-center justify-between mb-4 flex-wrap gap-2">
        <h1 className="text-2xl font-bold">Resources</h1>
        {/* filter row: 1 col on mobile, 3 cols on md+ */}
        <div
          className="flex flex-wrap gap-2 w-full md:w-auto md:flex-nowrap"
          data-testid="resource-filters"
        >
          <select
            aria-label="Filter by account"
            className="border rounded px-2 py-1 text-sm bg-white dark:bg-gray-900"
            value={accountFilter}
            onChange={(e) => setAccountFilter(e.target.value)}
          >
            <option value="">All accounts</option>
            {ACCOUNT_OPTIONS.map((a) => (
              <option key={a} value={a}>
                {a}
              </option>
            ))}
          </select>
          <select
            aria-label="Filter by region"
            className="border rounded px-2 py-1 text-sm bg-white dark:bg-gray-900"
            value={regionFilter}
            onChange={(e) => setRegionFilter(e.target.value)}
          >
            <option value="">All regions</option>
            {REGION_OPTIONS.map((r) => (
              <option key={r} value={r}>
                {r}
              </option>
            ))}
          </select>
          <select
            aria-label="Filter by type"
            className="border rounded px-2 py-1 text-sm bg-white dark:bg-gray-900"
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value)}
          >
            <option value="">All types</option>
            {TYPE_OPTIONS.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </div>
      </div>
      {err && <div className="text-red-500 mb-2">{err}</div>}
      {!resources && !err && <div>Loading…</div>}
      {resources && resources.length === 0 && (
        <div className="text-gray-500">No resources.</div>
      )}
      {resources && resources.length > 0 && (
        <div className="table-wrap bg-white dark:bg-gray-900 rounded shadow">
          <table className="dash" data-testid="resources-table">
            <thead>
              <tr>
                <th>Account</th>
                <th>Region</th>
                <th>Type</th>
                <th>ID</th>
                <th>Name</th>
                <th>Fetched</th>
              </tr>
            </thead>
            <tbody>
              {resources.map((r) => (
                <tr
                  key={`${r.account}-${r.region}-${r.type}-${r.id}`}
                  data-testid="resource-row"
                >
                  <td>{r.account}</td>
                  <td>{r.region}</td>
                  <td>{r.type}</td>
                  <td className="font-mono text-xs">{r.id}</td>
                  <td>{r.name}</td>
                  <td className="text-gray-500">{new Date(r.fetched_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
