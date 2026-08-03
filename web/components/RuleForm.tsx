'use client';

import { useState, FormEvent } from 'react';
import type { Rule } from '../lib/api';

export type RuleFormData = {
  account_alias: string;
  name: string;
  severity: 'critical' | 'warning' | 'info';
  metric: string;
  threshold?: number;
  channel: Record<string, unknown>;
  enabled: boolean;
};

type Props = {
  initial?: Partial<Rule>;
  onSubmit: (data: RuleFormData) => Promise<void>;
  submitLabel: string;
};

const SEVERITIES: Array<RuleFormData['severity']> = ['critical', 'warning', 'info'];

function defaultChannelText(channel?: Record<string, unknown>): string {
  if (!channel || Object.keys(channel).length === 0) return '';
  return JSON.stringify(channel, null, 2);
}

export default function RuleForm({ initial, onSubmit, submitLabel }: Props) {
  const [account, setAccount] = useState(initial?.account_alias ?? '');
  const [name, setName] = useState(initial?.name ?? '');
  const [severity, setSeverity] = useState<RuleFormData['severity']>(
    (initial?.severity as RuleFormData['severity']) ?? 'warning',
  );
  const [metric, setMetric] = useState(initial?.metric ?? '');
  const [threshold, setThreshold] = useState<string>(
    initial?.threshold != null ? String(initial.threshold) : '',
  );
  const [channelText, setChannelText] = useState<string>(
    defaultChannelText(initial?.channel as Record<string, unknown> | undefined),
  );
  const [enabled, setEnabled] = useState<boolean>(initial?.enabled ?? false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [submitting, setSubmitting] = useState(false);

  const validate = (): { ok: boolean; parsed?: RuleFormData; fieldErrors: Record<string, string> } => {
    const fieldErrors: Record<string, string> = {};
    if (!account.trim()) fieldErrors.account = 'account is required';
    if (!name.trim()) fieldErrors.name = 'name is required';
    if (!metric.trim()) fieldErrors.metric = 'metric is required';
    let parsedChannel: Record<string, unknown> = {};
    if (channelText.trim()) {
      try {
        parsedChannel = JSON.parse(channelText);
      } catch (e) {
        fieldErrors.channel = `invalid JSON: ${(e as Error).message}`;
      }
    }
    if (Object.keys(fieldErrors).length > 0) return { ok: false, fieldErrors };
    const parsed: RuleFormData = {
      account_alias: account.trim(),
      name: name.trim(),
      severity,
      metric: metric.trim(),
      threshold: threshold.trim() ? Number(threshold) : undefined,
      channel: parsedChannel,
      enabled,
    };
    return { ok: true, parsed, fieldErrors };
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setErrors({});
    const v = validate();
    if (!v.ok || !v.parsed) {
      setErrors(v.fieldErrors);
      return;
    }
    setSubmitting(true);
    try {
      await onSubmit(v.parsed);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form
      data-testid="rule-form"
      onSubmit={handleSubmit}
      className="bg-white dark:bg-gray-900 rounded shadow p-4 mb-4 grid grid-cols-1 md:grid-cols-2 gap-3"
    >
      <label className="flex flex-col text-sm">
        Account alias
        <input
          data-testid="input-account"
          value={account}
          onChange={(e) => setAccount(e.target.value)}
          className="mt-1 border rounded px-2 py-1 bg-white dark:bg-gray-800"
        />
        {errors.account && <span className="text-red-500 text-xs mt-1">{errors.account}</span>}
      </label>
      <label className="flex flex-col text-sm">
        Name
        <input
          data-testid="input-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="mt-1 border rounded px-2 py-1 bg-white dark:bg-gray-800"
        />
        {errors.name && <span className="text-red-500 text-xs mt-1">{errors.name}</span>}
      </label>
      <label className="flex flex-col text-sm">
        Severity
        <select
          data-testid="input-severity"
          value={severity}
          onChange={(e) => setSeverity(e.target.value as RuleFormData['severity'])}
          className="mt-1 border rounded px-2 py-1 bg-white dark:bg-gray-800"
        >
          {SEVERITIES.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
      </label>
      <label className="flex flex-col text-sm">
        Metric
        <input
          data-testid="input-metric"
          value={metric}
          onChange={(e) => setMetric(e.target.value)}
          className="mt-1 border rounded px-2 py-1 bg-white dark:bg-gray-800"
        />
        {errors.metric && <span className="text-red-500 text-xs mt-1">{errors.metric}</span>}
      </label>
      <label className="flex flex-col text-sm">
        Threshold (optional)
        <input
          data-testid="input-threshold"
          type="number"
          value={threshold}
          onChange={(e) => setThreshold(e.target.value)}
          className="mt-1 border rounded px-2 py-1 bg-white dark:bg-gray-800"
        />
      </label>
      <label className="flex flex-col text-sm md:col-span-2">
        Channel (JSON)
        <textarea
          data-testid="input-channel"
          value={channelText}
          onChange={(e) => setChannelText(e.target.value)}
          rows={4}
          className="mt-1 border rounded px-2 py-1 font-mono text-xs bg-white dark:bg-gray-800"
          placeholder='{"type":"webhook","url":"..."}'
        />
        {errors.channel && <span className="text-red-500 text-xs mt-1">{errors.channel}</span>}
      </label>
      <label className="flex items-center gap-2 text-sm md:col-span-2">
        <input
          data-testid="input-enabled"
          type="checkbox"
          checked={enabled}
          onChange={(e) => setEnabled(e.target.checked)}
        />
        Enabled
      </label>
      <div className="md:col-span-2">
        <button
          type="submit"
          data-testid="btn-submit"
          disabled={submitting}
          className="px-3 py-1.5 text-sm rounded bg-blue-600 text-white disabled:opacity-50"
        >
          {submitting ? 'Saving…' : submitLabel}
        </button>
      </div>
    </form>
  );
}