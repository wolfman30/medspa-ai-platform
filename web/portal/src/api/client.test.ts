import { afterEach, describe, expect, it, vi } from 'vitest';
import { updateClinicConfig } from './client';

describe('api client error handling', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('surfaces non-JSON error responses', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      text: async () => 'invalid onboarding token',
    });
    vi.stubGlobal('fetch', fetchMock);

    await expect(updateClinicConfig('org_123', { services: ['Botox'] }))
      .rejects.toThrow('invalid onboarding token');
  });

  it('surfaces JSON error responses', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      text: async () => JSON.stringify({ error: 'bad request' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await expect(updateClinicConfig('org_123', { services: ['Botox'] }))
      .rejects.toThrow('bad request');
  });
});
