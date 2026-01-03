const ORG_ID_STORAGE_KEY = 'medspa_org_id';
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
