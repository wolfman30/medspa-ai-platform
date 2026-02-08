import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Dashboard } from './Dashboard';
import {
  getPortalOverview,
  getClinicConfig,
  getSquareConnectUrl,
  getSquareStatus,
  listConversations,
  listDeposits,
  type PortalDashboardOverview,
} from '../api/client';

vi.mock('../api/client', () => ({
  getPortalOverview: vi.fn(),
  getClinicConfig: vi.fn(),
  getSquareConnectUrl: vi.fn(),
  getSquareStatus: vi.fn(),
  listConversations: vi.fn(),
  listDeposits: vi.fn(),
}));

const sampleStats: PortalDashboardOverview = {
  org_id: 'org_123',
  period_start: 'all-time',
  period_end: 'now',
  conversations: 8,
  successful_deposits: 2,
  total_collected_cents: 12500,
  conversion_pct: 25,
};

const sampleConversations = {
  conversations: [
    {
      id: 'conv_1',
      org_id: 'org_123',
      customer_phone: '+15551234567',
      status: 'active',
      message_count: 3,
      customer_message_count: 1,
      ai_message_count: 2,
      started_at: '2026-01-01T10:00:00Z',
      last_message_at: '2026-01-01T10:05:00Z',
    },
  ],
  total: 1,
  page: 1,
  page_size: 5,
  total_pages: 1,
};

const sampleDeposits = {
  deposits: [
    {
      id: 'dep_1',
      org_id: 'org_123',
      lead_phone: '+15559876543',
      lead_name: 'Jane Doe',
      amount_cents: 7500,
      status: 'succeeded',
      provider: 'square',
      created_at: '2026-01-01T11:00:00Z',
    },
  ],
  total: 1,
  page: 1,
  page_size: 5,
  total_pages: 1,
};

const sampleSquareStatus = {
  connected: true,
  org_id: 'org_123',
  merchant_id: 'merchant_123',
  location_id: 'loc_123',
  token_expires_at: '2026-02-01T00:00:00Z',
  token_expired: false,
  refresh_token_present: true,
  connected_at: '2026-01-01T09:00:00Z',
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
    vi.mocked(getClinicConfig).mockResolvedValue({ booking_platform: 'square' } as any);
    vi.mocked(getSquareStatus).mockResolvedValue(sampleSquareStatus);
    vi.mocked(getSquareConnectUrl).mockResolvedValue('https://example.com/connect');
    vi.mocked(listConversations).mockResolvedValue(sampleConversations);
    vi.mocked(listDeposits).mockResolvedValue(sampleDeposits);

    render(<Dashboard orgId="org_123" />);

    expect(screen.getByText('Loading dashboard...')).toBeInTheDocument();

    deferred.resolve(sampleStats);
    await screen.findByText('Dashboard');
  });

  it('renders dashboard metrics when data loads', async () => {
    vi.mocked(getPortalOverview).mockResolvedValue(sampleStats);
    vi.mocked(getClinicConfig).mockResolvedValue({ booking_platform: 'square' } as any);
    vi.mocked(getSquareStatus).mockResolvedValue(sampleSquareStatus);
    vi.mocked(getSquareConnectUrl).mockResolvedValue('https://example.com/connect');
    vi.mocked(listConversations).mockResolvedValue(sampleConversations);
    vi.mocked(listDeposits).mockResolvedValue(sampleDeposits);

    render(<Dashboard orgId="org_123" />);

    expect(await screen.findByText('Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Conversations')).toBeInTheDocument();
    expect(screen.getByText('Deposits Collected')).toBeInTheDocument();
    expect(screen.getByText('Total Collected')).toBeInTheDocument();
    expect(screen.getByText('8')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.getByText('$125.00')).toBeInTheDocument();
    expect(screen.getByText('25.0%')).toBeInTheDocument();
  });

  it('shows an error message when the request fails', async () => {
    vi.mocked(getPortalOverview).mockRejectedValue(new Error('No data'));
    vi.mocked(getClinicConfig).mockResolvedValue({ booking_platform: 'square' } as any);
    vi.mocked(getSquareStatus).mockResolvedValue(sampleSquareStatus);
    vi.mocked(getSquareConnectUrl).mockResolvedValue('https://example.com/connect');
    vi.mocked(listConversations).mockResolvedValue(sampleConversations);
    vi.mocked(listDeposits).mockResolvedValue(sampleDeposits);

    render(<Dashboard orgId="org_123" />);

    expect(await screen.findByText('No data')).toBeInTheDocument();
  });
});
