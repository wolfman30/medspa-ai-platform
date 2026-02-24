const ORG_ID_STORAGE_KEY = 'medspa_org_id';
const SETUP_COMPLETE_PREFIX = 'medspa_setup_complete:';
const LEGACY_KEYS = ['org_id'];

export function getStoredOrgId(): string | null {
  if (typeof window === 'undefined') return null;
  const stored = window.localStorage.getItem(ORG_ID_STORAGE_KEY);
  if (stored) return stored;
  for (const key of LEGACY_KEYS) {
    const legacy = window.localStorage.getItem(key);
    if (legacy) {
      window.localStorage.setItem(ORG_ID_STORAGE_KEY, legacy);
      return legacy;
    }
  }
  return null;
}

export function setStoredOrgId(orgId: string) {
  if (typeof window === 'undefined') return;
  const trimmed = orgId.trim();
  if (trimmed) {
    window.localStorage.setItem(ORG_ID_STORAGE_KEY, trimmed);
  }
}

export function getStoredSetupComplete(orgId: string): boolean {
  if (typeof window === 'undefined') return false;
  const trimmed = orgId.trim();
  if (!trimmed) return false;
  return window.localStorage.getItem(`${SETUP_COMPLETE_PREFIX}${trimmed}`) === 'true';
}

export function setStoredSetupComplete(orgId: string) {
  if (typeof window === 'undefined') return;
  const trimmed = orgId.trim();
  if (trimmed) {
    window.localStorage.setItem(`${SETUP_COMPLETE_PREFIX}${trimmed}`, 'true');
  }
}
