// Base URL of the Archaeologist Go service.
//
// Resolution order:
//   1. localStorage override (the "API" field in the top bar, survives reloads)
//   2. VITE_API_BASE_URL build-time env
//   3. default: the local service on :8080

const STORAGE_KEY = 'archaeologist.apiBase';
export const DEFAULT_API_BASE = 'http://localhost:8080';

function readStored(): string | null {
  try {
    const v = window.localStorage.getItem(STORAGE_KEY)?.trim();
    return v || null;
  } catch {
    return null;
  }
}

export function getApiBase(): string {
  const fromEnv = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.trim();
  return readStored() || fromEnv || DEFAULT_API_BASE;
}

export function setApiBase(value: string): void {
  try {
    const v = value.trim();
    if (v) window.localStorage.setItem(STORAGE_KEY, v);
    else window.localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* storage unavailable — in-memory only */
  }
}
