'use client';
import { useEffect, useState, useCallback } from 'react';
import { useRouter, useParams } from 'next/navigation';
import { api, Rule } from '../../../lib/api';
import RuleForm, { RuleFormData } from '../../../components/RuleForm';

export default function RuleEditPage() {
  const router = useRouter();
  const params = useParams<{ id: string }>();
  const [rule, setRule] = useState<Rule | null | undefined>(undefined);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(() => {
    const id = Number(params.id);
    api.listRules()
      .then((all) => {
        const found = all.find((r) => r.id === id) ?? null;
        setRule(found);
      })
      .catch((e) => setErr(String(e)));
  }, [params.id]);

  useEffect(() => {
    load();
  }, [load]);

  if (err) return <div className="text-red-500">{err}</div>;
  if (rule === undefined) return <div>Loading…</div>;
  if (rule === null) {
    return (
      <section>
        <h1 className="text-2xl font-bold mb-4">Edit rule</h1>
        <div className="text-gray-500" data-testid="rule-not-found">Rule not found.</div>
      </section>
    );
  }

  const handleSave = async (data: RuleFormData) => {
    try {
      await api.updateRule(rule.id, data as Partial<Rule>);
      router.push('/rules');
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <section>
      <h1 className="text-2xl font-bold mb-4">Edit rule #{rule.id}</h1>
      <RuleForm initial={rule} onSubmit={handleSave} submitLabel="Update" />
    </section>
  );
}