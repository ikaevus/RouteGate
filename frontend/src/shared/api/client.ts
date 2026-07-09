const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';
export const AUTH_TOKEN_STORAGE_KEY = 'routegate.auth.token';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

interface ApiErrorPayload {
  status?: string;
  message?: string;
  error?: string;
}

export function getAuthToken(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }

  return window.localStorage.getItem(AUTH_TOKEN_STORAGE_KEY);
}

export function setAuthToken(token: string): void {
  if (typeof window === 'undefined') {
    return;
  }

  window.localStorage.setItem(AUTH_TOKEN_STORAGE_KEY, token);
}

export function clearAuthToken(): void {
  if (typeof window === 'undefined') {
    return;
  }

  window.localStorage.removeItem(AUTH_TOKEN_STORAGE_KEY);
}

function buildHeaders(extraHeaders?: HeadersInit): HeadersInit {
  const token = getAuthToken();

  return {
    Accept: 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...extraHeaders,
  };
}

async function buildApiError(response: Response, method: string, path: string): Promise<ApiError> {
  const fallback = `API request failed: ${method} ${path} returned ${response.status}`;

  try {
    const text = await response.text();
    if (!text) {
      return new ApiError(fallback, response.status);
    }

    try {
      const payload = JSON.parse(text) as ApiErrorPayload;
      const message = typeof payload.message === 'string' && payload.message.trim() !== ''
        ? payload.message
        : fallback;
      const code = typeof payload.status === 'string' && payload.status.trim() !== ''
        ? payload.status
        : undefined;
      return new ApiError(message, response.status, code);
    } catch {
      return new ApiError(text, response.status);
    }
  } catch {
    return new ApiError(fallback, response.status);
  }
}

export async function apiGet<TResponse>(path: string): Promise<TResponse> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'GET',
    headers: buildHeaders(),
  });

  if (!response.ok) {
    throw await buildApiError(response, 'GET', path);
  }

  return response.json() as Promise<TResponse>;
}

export async function apiPost<TRequest, TResponse>(
  path: string,
  body?: TRequest,
): Promise<TResponse> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'POST',
    headers: buildHeaders({
      'Content-Type': 'application/json',
    }),
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  if (!response.ok) {
    throw await buildApiError(response, 'POST', path);
  }

  return response.json() as Promise<TResponse>;
}

export async function apiPut<TRequest, TResponse>(
  path: string,
  body: TRequest,
): Promise<TResponse> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'PUT',
    headers: buildHeaders({
      'Content-Type': 'application/json',
    }),
    body: JSON.stringify(body),
  });

  if (!response.ok) {
    throw await buildApiError(response, 'PUT', path);
  }

  return response.json() as Promise<TResponse>;
}

export async function apiPatch<TRequest, TResponse>(
  path: string,
  body: TRequest,
): Promise<TResponse> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'PATCH',
    headers: buildHeaders({
      'Content-Type': 'application/json',
    }),
    body: JSON.stringify(body),
  });

  if (!response.ok) {
    throw await buildApiError(response, 'PATCH', path);
  }

  return response.json() as Promise<TResponse>;
}

export async function apiDelete(path: string): Promise<void> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'DELETE',
    headers: buildHeaders(),
  });

  if (!response.ok) {
    throw await buildApiError(response, 'DELETE', path);
  }
}
