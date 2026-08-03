import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, cleanup } from '@testing-library/react';
import StatsPage from '../app/stats/page';

function mockJsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const FIXTURE = {
  total_alerts: 1234,
  open_alerts: 42,
  ai_success_rate: 0.87,
  avg_latency_ms: 850,
  resources_covered: 578,
  generated_at: '2026-08-03T12:34:56Z',
};

beforeEach(() => {
  (globalThis as { fetch: typeof fetch }).fetch = vi.fn(() =>
    Promise.resolve(mockJsonResponse(FIXTURE)),
  ) as unknown as typeof fetch;
});

afterEach(() => {
  vi.restoreAllMocks();
  cleanup();
});

describe('StatsPage', () => {
  it('renders "Loading…" while fetching', async () => {
    let resolve!: (v: Response) => void;
    (globalThis as { fetch: typeof fetch }).fetch = vi.fn(
      () => new Promise<Response>((r) => (resolve = r)),
    ) as unknown as typeof fetch;
    render(<StatsPage />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    resolve(mockJsonResponse(FIXTURE));
    await waitFor(() => expect(screen.queryByText('Loading…')).not.toBeInTheDocument());
  });

  it('renders all 5 stat cards with numbers from the fixture', async () => {
    render(<StatsPage />);
    await waitFor(() => expect(screen.getByTestId('stats-grid')).toBeInTheDocument());
    const cards = screen.getAllByTestId('stat-card');
    expect(cards).toHaveLength(5);

    // toLocaleString() formats with thousands separators in en-US.
    expect(screen.getByText('1,234')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();
    expect(screen.getByText('87%')).toBeInTheDocument();
    expect(screen.getByText('850 ms')).toBeInTheDocument();
    expect(screen.getByText('578')).toBeInTheDocument();
  });

  it('shows generated_at timestamp', async () => {
    render(<StatsPage />);
    await waitFor(() => expect(screen.getByTestId('stats-grid')).toBeInTheDocument());
    // The exact format depends on locale, but the year substring should appear.
    expect(screen.getByText(/2026/)).toBeInTheDocument();
  });

  it('open_alerts card links to /alerts', async () => {
    render(<StatsPage />);
    await waitFor(() => expect(screen.getByTestId('stats-grid')).toBeInTheDocument());
    const link = screen.getByRole('link', { name: /42/ });
    expect(link).toHaveAttribute('href', '/alerts');
  });
});
