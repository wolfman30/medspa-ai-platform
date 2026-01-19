import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Dashboard } from './Dashboard';
import { getPortalOverview, type PortalDashboardOverview } from '../api/client';

vi.mock('../api/client', () => ({
  getPortalOverview: vi.fn(),
}));

const sampleStats: PortalDashboardOverview = {
  org_id: 'org_123',
  period_start: 'all-time',
  period_end: 'now',
  conversations: 8,
  successful_deposits: 2,
  conversion_pct: 25,
};

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('Dashboard', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows the loading state initially', async () => {
    const deferred = createDeferred<PortalDashboardOverview>();
    vi.mocked(getPortalOverview).mockReturnValue(deferred.promise);

    render(<Dashboard orgId="org_123" />);

    expect(screen.getByText('Loading dashboard...')).toBeInTheDocument();

    deferred.resolve(sampleStats);
    await screen.findByText('Test Dashboard');
  });

  it('renders dashboard metrics when data loads', async () => {
    vi.mocked(getPortalOverview).mockResolvedValue(sampleStats);

    render(<Dashboard orgId="org_123" />);

    expect(await screen.findByText('Test Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Conversations')).toBeInTheDocument();
    expect(screen.getByText('Successful Deposits')).toBeInTheDocument();
    expect(screen.getByText('8')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('25.0%')).toBeInTheDocument();
  });

  it('shows an error message when the request fails', async () => {
    vi.mocked(getPortalOverview).mockRejectedValue(new Error('No data'));

    render(<Dashboard orgId="org_123" />);

    expect(await screen.findByText('No data')).toBeInTheDocument();
  });
});
