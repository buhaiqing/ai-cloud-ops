import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';
import ResourcesPage from '../app/resources/page';

function mockJsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyFetchMock = any;

function asFetchMock(fn: AnyFetchMock): typeof fetch {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return fn as unknown as typeof fetch;
}

const FIXTURE = [
  {
    account: 'prod',
    region: 'cn-hangzhou',
    type: 'ecs',
    id: 'i-abc123',
    name: 'web-01',
    fetched_at: '2026-08-03T10:00:00Z',
  },
  {
    account: 'prod',
    region: 'cn-shanghai',
    type: 'rds',
    id: 'rm-def456',
    name: 'db-primary',
    fetched_at: '2026-08-03T10:01:00Z',
  },
];

beforeEach(() => {
  // default: empty list
  (globalThis as { fetch: typeof fetch }).fetch = asFetchMock(
    vi.fn(() => Promise.resolve(mockJsonResponse([]))),
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  cleanup();
});

describe('ResourcesPage', () => {
  it('renders "Loading…" while fetching', async () => {
    let resolve!: (v: Response) => void;
    (globalThis as { fetch: typeof fetch }).fetch = asFetchMock(
      vi.fn(() => new Promise<Response>((r) => (resolve = r))),
    );
    render(<ResourcesPage />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    resolve(mockJsonResponse([]));
    await waitFor(() => expect(screen.queryByText('Loading…')).not.toBeInTheDocument());
  });

  it('renders "No resources." when the list is empty', async () => {
    render(<ResourcesPage />);
    await waitFor(() => expect(screen.getByText('No resources.')).toBeInTheDocument());
    expect(screen.queryByTestId('resources-table')).not.toBeInTheDocument();
  });

  it('renders a row per resource in the fixture', async () => {
    (globalThis as { fetch: typeof fetch }).fetch = asFetchMock(
      vi.fn(() => Promise.resolve(mockJsonResponse(FIXTURE))),
    );
    render(<ResourcesPage />);
    await waitFor(() => expect(screen.getByTestId('resources-table')).toBeInTheDocument());
    const rows = screen.getAllByTestId('resource-row');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('i-abc123');
    expect(rows[0]).toHaveTextContent('web-01');
    expect(rows[0]).toHaveTextContent('prod');
    expect(rows[0]).toHaveTextContent('cn-hangzhou');
    expect(rows[0]).toHaveTextContent('ecs');
    expect(rows[1]).toHaveTextContent('rm-def456');
    expect(rows[1]).toHaveTextContent('db-primary');
  });

  it('changing a filter dropdown re-fetches with the correct query string', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(mockJsonResponse([])));
    (globalThis as { fetch: typeof fetch }).fetch = asFetchMock(fetchMock);

    render(<ResourcesPage />);
    // initial fetch with no params
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const initialUrl = String((fetchMock.mock.calls[0] as any[])[0] ?? '');
    expect(initialUrl).toContain('/api/v1/resources');
    expect(initialUrl).not.toContain('account=');

    // change account filter
    const accountSelect = screen.getByLabelText(/account/i) as HTMLSelectElement;
    fireEvent.change(accountSelect, { target: { value: 'prod' } });
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(1));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    expect(String((fetchMock.mock.calls[1] as any[])[0] ?? '')).toContain('account=prod');

    // change type filter — should include previous account param too
    const typeSelect = screen.getByLabelText(/type/i) as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: 'ecs' } });
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThan(2));
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const thirdUrl = String((fetchMock.mock.calls[2] as any[])[0] ?? '');
    expect(thirdUrl).toContain('account=prod');
    expect(thirdUrl).toContain('type=ecs');
  });

  it('renders an error message when fetch fails', async () => {
    (globalThis as { fetch: typeof fetch }).fetch = asFetchMock(
      vi.fn(() => Promise.resolve(new Response('boom', { status: 500 }))),
    );
    render(<ResourcesPage />);
    await waitFor(() => expect(screen.getByText(/500/)).toBeInTheDocument());
  });
});
