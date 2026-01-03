import { afterEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Dashboard } from './Dashboard';
import { getDashboardStats, type DashboardStats } from '../api/client';

vi.mock('../api/client', () => ({
  getDashboardStats: vi.fn(),
}));

const sampleStats: DashboardStats = {
  org_id: 'org_123',
  org_name: 'MedSpa',
  period: 'week',
  leads: {
    total: 10,
    new_this_week: 2,
    conversion_rate: 25,
  },
  conversations: {
    unique_conversations: 5,
    total_jobs: 8,
    today: 1,
    this_week: 4,
  },
  payments: {
    total_collected_cents: 123456,
    this_week_cents: 23456,
    pending_deposits: 1,
    refunded_cents: 0,
    dispute_count: 0,
  },
  bookings: {
    total: 2,
    upcoming: 1,
    this_week: 1,
    cancelled_count: 0,
  },
  compliance: {
    audit_events_today: 0,
    supervisor_interventions: 0,
    phi_detections: 0,
    disclaimers_sent: 0,
  },
  onboarding: {
    brand_status: 'VERIFIED',
    campaign_status: 'ACTIVE',
    numbers_active: 1,
    fully_compliant: true,
  },
  pending_actions: [],
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
    const deferred = createDeferred<DashboardStats>();
    vi.mocked(getDashboardStats).mockReturnValue(deferred.promise);

    render(<Dashboard orgId="org_123" />);

    expect(screen.getByText('Loading ROI data...')).toBeInTheDocument();

    deferred.resolve(sampleStats);
    await screen.findByText('Performance Dashboard');
  });

  it('renders dashboard metrics when data loads', async () => {
    vi.mocked(getDashboardStats).mockResolvedValue(sampleStats);

    render(<Dashboard orgId="org_123" />);

    expect(await screen.findByText('Performance Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Total Revenue Captured')).toBeInTheDocument();
    expect(screen.getByText('$1234.56')).toBeInTheDocument();
    expect(screen.getByText('4')).toBeInTheDocument();
    expect(screen.getByText('25.0%')).toBeInTheDocument();
  });

  it('shows an error message when the request fails', async () => {
    vi.mocked(getDashboardStats).mockRejectedValue(new Error('No data'));

    render(<Dashboard orgId="org_123" />);

    expect(await screen.findByText('No data')).toBeInTheDocument();
  });
});
