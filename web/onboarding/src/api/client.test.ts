import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getDashboardStats, getPortalOverview } from './client';

vi.mock('../auth/config', () => ({
  isCognitoConfigured: () => false,
}));

const sampleResponse = {
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

const samplePortalResponse = {
  org_id: 'org_123',
  period_start: 'all-time',
  period_end: 'now',
  conversations: 12,
  successful_deposits: 3,
  conversion_pct: 25,
};

describe('getDashboardStats', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it('calls the dashboard endpoint with headers and returns JSON', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue({
      ok: true,
      json: vi.fn().mockResolvedValue(sampleResponse),
    } as Response);

    const result = await getDashboardStats('org_123');

    expect(fetchMock).toHaveBeenCalledWith(
      'http://localhost:8080/admin/orgs/org_123/dashboard',
      {
        headers: {
          'Content-Type': 'application/json',
        },
      }
    );
    expect(result).toEqual(sampleResponse);
  });

  it('throws when the response is not ok', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue({
      ok: false,
      json: vi.fn().mockResolvedValue({ error: 'Nope' }),
    } as Response);

    await expect(getDashboardStats('org_123')).rejects.toThrow('Nope');
  });
});

describe('getPortalOverview', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  it('calls the portal overview endpoint with filters', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue({
      ok: true,
      json: vi.fn().mockResolvedValue(samplePortalResponse),
    } as Response);

    const result = await getPortalOverview('org_123', {
      start: '2025-01-01T00:00:00Z',
      end: '2025-01-08T00:00:00Z',
      phone: '5005550001',
    });

    expect(fetchMock).toHaveBeenCalledWith(
      'http://localhost:8080/portal/orgs/org_123/dashboard?start=2025-01-01T00%3A00%3A00Z&end=2025-01-08T00%3A00%3A00Z&phone=5005550001',
      {
        headers: {
          'Content-Type': 'application/json',
        },
      }
    );
    expect(result).toEqual(samplePortalResponse);
  });

  it('throws when the response is not ok', async () => {
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValue({
      ok: false,
      json: vi.fn().mockResolvedValue({ error: 'Nope' }),
    } as Response);

    await expect(getPortalOverview('org_123')).rejects.toThrow('Nope');
  });
});
