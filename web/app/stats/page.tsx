'use client';
import { useEffect, useState } from 'react';
import Link from 'next/link';
import { api, Stats } from '../../lib/api';

type Card = { label: string; value: string; subtext?: string; href?: string };

export default function StatsPage() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    api.stats().then(setStats).catch((e) => setErr(String(e)));
  }, []);

  if (err) {
    return (
      <section>
        <h1 className="text-2xl font-bold mb-4">Stats</h1>
        <div className="text-red-500">{err}</div>
      </section>
    );
  }

  if (!stats) {
    return (
      <section>
        <h1 className="text-2xl font-bold mb-4">Stats</h1>
        <div>Loading…</div>
      </section>
    );
  }

  const cards: Card[] = [
    { label: 'Total alerts', value: stats.total_alerts.toLocaleString() },
    {
      label: 'Open alerts',
      value: stats.open_alerts.toLocaleString(),
      href: '/alerts',
    },
    {
      label: 'AI success rate',
      value: `${Math.round(stats.ai_success_rate * 100)}%`,
      subtext: 'last 24h',
    },
    {
      label: 'Avg latency',
      value: `${stats.avg_latency_ms.toLocaleString()} ms`,
      subtext: 'AI analysis',
    },
    { label: 'Resources covered', value: stats.resources_covered.toLocaleString() },
  ];

  return (
    <section>
      <h1 className="text-2xl font-bold mb-4">Stats</h1>
      <div
        className="grid grid-cols-1 md:grid-cols-5 gap-4"
        data-testid="stats-grid"
      >
        {cards.map((c) => {
          const inner = (
            <>
              <div className="text-sm text-gray-500 dark:text-gray-400">{c.label}</div>
              <div className="text-3xl font-bold mt-1">{c.value}</div>
              {c.subtext && (
                <div className="text-xs text-gray-400 mt-1">{c.subtext}</div>
              )}
            </>
          );
          return (
            <div
              key={c.label}
              data-testid="stat-card"
              className="bg-white dark:bg-gray-900 rounded shadow p-4"
            >
              {c.href ? (
                <Link href={c.href} className="block hover:opacity-80">
                  {inner}
                </Link>
              ) : (
                inner
              )}
            </div>
          );
        })}
      </div>
      <div className="text-xs text-gray-400 mt-6">
        Generated at {new Date(stats.generated_at).toLocaleString()}
      </div>
    </section>
  );
}
