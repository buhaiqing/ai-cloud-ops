import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import RuleForm from '../components/RuleForm';
import RulesPage from '../app/rules/page';
import RuleEditPage from '../app/rules/[id]/page';

// Mock fetch globally. Tests build URLs to inspect.
function mockJsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const useParamsMock = vi.fn(() => ({ id: '1' }));
vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
  useParams: () => useParamsMock(),
}));

const baseRule = {
  account_alias: 'prod',
  name: 'High CPU',
  severity: 'critical' as const,
  metric: 'cpu_usage',
  threshold: 80,
  channel: { type: 'webhook', url: 'https://example.com/hook' },
  enabled: true,
};

beforeEach(() => {
  // default: empty
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).fetch = vi.fn(() => Promise.resolve(mockJsonResponse([])));
  vi.spyOn(window, 'confirm').mockReturnValue(true);
  useParamsMock.mockReset();
  useParamsMock.mockImplementation(() => ({ id: '1' }));
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('RuleForm', () => {
  it('renders all fields with initial values', () => {
    render(
      <RuleForm
        initial={baseRule}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        submitLabel="Save"
      />,
    );
    expect(screen.getByTestId('rule-form')).toBeInTheDocument();
    expect(screen.getByTestId('input-account')).toHaveValue('prod');
    expect(screen.getByTestId('input-name')).toHaveValue('High CPU');
    expect(screen.getByTestId('input-severity')).toHaveValue('critical');
    expect(screen.getByTestId('input-metric')).toHaveValue('cpu_usage');
    expect(screen.getByTestId('input-threshold')).toHaveValue(80);
    expect(screen.getByTestId('input-channel')).toHaveValue(
      JSON.stringify(baseRule.channel, null, 2),
    );
    expect(screen.getByTestId('input-enabled')).toBeChecked();
    expect(screen.getByTestId('btn-submit')).toHaveTextContent('Save');
  });

  it('renders empty form when no initial provided', () => {
    render(
      <RuleForm onSubmit={vi.fn().mockResolvedValue(undefined)} submitLabel="Create" />,
    );
    expect(screen.getByTestId('input-account')).toHaveValue('');
    expect(screen.getByTestId('input-name')).toHaveValue('');
    expect(screen.getByTestId('input-severity')).toHaveValue('warning');
    expect(screen.getByTestId('input-channel')).toHaveValue('');
    expect(screen.getByTestId('input-enabled')).not.toBeChecked();
  });

  it('shows error when channel is not valid JSON', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<RuleForm onSubmit={onSubmit} submitLabel="Save" />);
    fireEvent.change(screen.getByTestId('input-account'), { target: { value: 'prod' } });
    fireEvent.change(screen.getByTestId('input-name'), { target: { value: 'Test rule' } });
    fireEvent.change(screen.getByTestId('input-metric'), { target: { value: 'cpu' } });
    fireEvent.change(screen.getByTestId('input-channel'), { target: { value: '{bad json' } });
    fireEvent.click(screen.getByTestId('btn-submit'));
    await waitFor(() => {
      expect(screen.getByText(/invalid json/i)).toBeInTheDocument();
    });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('calls onSubmit with parsed values on valid submit', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<RuleForm onSubmit={onSubmit} submitLabel="Save" />);
    fireEvent.change(screen.getByTestId('input-account'), { target: { value: 'prod' } });
    fireEvent.change(screen.getByTestId('input-name'), { target: { value: 'High CPU' } });
    fireEvent.change(screen.getByTestId('input-severity'), { target: { value: 'critical' } });
    fireEvent.change(screen.getByTestId('input-metric'), { target: { value: 'cpu_usage' } });
    fireEvent.change(screen.getByTestId('input-threshold'), { target: { value: '90' } });
    fireEvent.change(screen.getByTestId('input-channel'), {
      target: { value: '{"type":"webhook","url":"https://x"}' },
    });
    fireEvent.click(screen.getByTestId('input-enabled'));
    fireEvent.click(screen.getByTestId('btn-submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit).toHaveBeenCalledWith({
      account_alias: 'prod',
      name: 'High CPU',
      severity: 'critical',
      metric: 'cpu_usage',
      threshold: 90,
      channel: { type: 'webhook', url: 'https://x' },
      enabled: true, // toggled off then back on
    });
  });

  it('shows required-field error when name missing', async () => {
    render(<RuleForm onSubmit={vi.fn().mockResolvedValue(undefined)} submitLabel="Save" />);
    fireEvent.change(screen.getByTestId('input-account'), { target: { value: 'prod' } });
    fireEvent.change(screen.getByTestId('input-metric'), { target: { value: 'cpu' } });
    fireEvent.click(screen.getByTestId('btn-submit'));
    await waitFor(() => {
      expect(screen.getByText(/name is required/i)).toBeInTheDocument();
    });
  });

  it('omits threshold when blank', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<RuleForm onSubmit={onSubmit} submitLabel="Save" />);
    fireEvent.change(screen.getByTestId('input-account'), { target: { value: 'prod' } });
    fireEvent.change(screen.getByTestId('input-name'), { target: { value: 'Test' } });
    fireEvent.change(screen.getByTestId('input-metric'), { target: { value: 'cpu' } });
    fireEvent.click(screen.getByTestId('btn-submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalled());
    const arg = onSubmit.mock.calls[0][0];
    expect(arg.threshold).toBeUndefined();
  });
});

const FIXTURE_RULES = [
  {
    id: 1,
    account_alias: 'prod',
    name: 'High CPU',
    severity: 'critical',
    metric: 'cpu_usage',
    threshold: 80,
    channel: { type: 'webhook', url: 'https://x' },
    enabled: true,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
  },
  {
    id: 2,
    account_alias: 'staging',
    name: 'Disk fill',
    severity: 'warning',
    metric: 'disk_usage',
    threshold: 70,
    channel: { type: 'email', to: 'ops@example.com' },
    enabled: false,
    created_at: '2026-08-02T00:00:00Z',
    updated_at: '2026-08-02T00:00:00Z',
  },
];

describe('RulesPage', () => {
  it('renders rows from fixture (2 rules)', async () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(() => Promise.resolve(mockJsonResponse(FIXTURE_RULES)));
    render(<RulesPage />);
    await waitFor(() => expect(screen.getByTestId('rules-table')).toBeInTheDocument());
    const rows = screen.getAllByTestId('rule-row');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('High CPU');
    expect(rows[0]).toHaveTextContent('prod');
    expect(rows[0]).toHaveTextContent('critical');
    expect(rows[1]).toHaveTextContent('Disk fill');
    expect(rows[1]).toHaveTextContent('staging');
  });

  it('shows empty state when no rules', async () => {
    render(<RulesPage />);
    await waitFor(() => expect(screen.getByText(/no rules/i)).toBeInTheDocument());
  });

  it('delete button triggers window.confirm and api.deleteRule on accept', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }));
      return Promise.resolve(mockJsonResponse(FIXTURE_RULES));
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    vi.spyOn(window, 'confirm').mockReturnValue(true);

    render(<RulesPage />);
    await waitFor(() => expect(screen.getByTestId('rules-table')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('btn-delete-1'));
    await waitFor(() => {
      const deleteCall = fetchMock.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'DELETE',
      );
      expect(deleteCall).toBeDefined();
      expect(deleteCall![0]).toContain('/api/v1/rules/1');
    });
  });

  it('does NOT call api.deleteRule when confirm is cancelled', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') return Promise.resolve(new Response(null, { status: 204 }));
      return Promise.resolve(mockJsonResponse(FIXTURE_RULES));
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    vi.spyOn(window, 'confirm').mockReturnValue(false);

    render(<RulesPage />);
    await waitFor(() => expect(screen.getByTestId('rules-table')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('btn-delete-1'));
    // give the (would-be) handler a chance to fire
    await new Promise((r) => setTimeout(r, 50));
    const deleteCall = fetchMock.mock.calls.find(
      (c) => (c[1] as RequestInit | undefined)?.method === 'DELETE',
    );
    expect(deleteCall).toBeUndefined();
  });

  it('clicking "+ New rule" toggles inline RuleForm in create mode', async () => {
    render(<RulesPage />);
    expect(screen.queryByTestId('rule-form')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('btn-new'));
    expect(screen.getByTestId('rule-form')).toBeInTheDocument();
    expect(screen.getByTestId('btn-submit')).toBeInTheDocument();
  });

  it('submitting new-rule form calls api.createRule and refreshes list', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'POST') {
        return Promise.resolve(mockJsonResponse({ id: 99 }, 201));
      }
      return Promise.resolve(mockJsonResponse(FIXTURE_RULES));
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    render(<RulesPage />);
    await waitFor(() => expect(screen.getByTestId('rules-table')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('btn-new'));
    fireEvent.change(screen.getByTestId('input-account'), { target: { value: 'prod' } });
    fireEvent.change(screen.getByTestId('input-name'), { target: { value: 'New rule' } });
    fireEvent.change(screen.getByTestId('input-metric'), { target: { value: 'mem' } });
    fireEvent.click(screen.getByTestId('btn-submit'));
    await waitFor(() => {
      const post = fetchMock.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'POST',
      );
      expect(post).toBeDefined();
      expect(post![0]).toContain('/api/v1/rules');
    });
  });
});

describe('RuleEditPage', () => {
  it('pre-fills form with rule data and submits to updateRule', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === 'PUT') return Promise.resolve(mockJsonResponse({ id: 42, updated: true }));
      return Promise.resolve(mockJsonResponse(FIXTURE_RULES));
    });
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = fetchMock;
    render(<RuleEditPage />);
    await waitFor(() => expect(screen.getByTestId('rule-form')).toBeInTheDocument());
    expect(screen.getByTestId('input-account')).toHaveValue('prod');
    expect(screen.getByTestId('input-name')).toHaveValue('High CPU');
    expect(screen.getByTestId('input-severity')).toHaveValue('critical');
    expect(screen.getByTestId('input-metric')).toHaveValue('cpu_usage');
    // change a field and submit
    fireEvent.change(screen.getByTestId('input-name'), { target: { value: 'High CPU v2' } });
    fireEvent.click(screen.getByTestId('btn-submit'));
    await waitFor(() => {
      const put = fetchMock.mock.calls.find(
        (c) => (c[1] as RequestInit | undefined)?.method === 'PUT',
      );
      expect(put).toBeDefined();
      expect(put![0]).toContain('/api/v1/rules/1');
      const body = JSON.parse((put![1] as RequestInit).body as string);
      expect(body.name).toBe('High CPU v2');
    });
  });

  it('shows "Rule not found" when id not present in list', async () => {
    useParamsMock.mockImplementation(() => ({ id: '99' }));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (globalThis as any).fetch = vi.fn(() => Promise.resolve(mockJsonResponse(FIXTURE_RULES)));
    render(<RuleEditPage />);
    await waitFor(() => {
      expect(screen.getByText(/rule not found/i)).toBeInTheDocument();
    });
  });
});