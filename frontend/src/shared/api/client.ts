const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

function getAuthToken(): string | null {
  return localStorage.getItem('routegate.auth.token');
}

function buildHeaders(extraHeaders?: HeadersInit): HeadersInit {
  const token = getAuthToken();

  return {
    Accept: 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
    ...extraHeaders,
  };
}

export async function apiGet<TResponse>(path: string): Promise<TResponse> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'GET',
    headers: buildHeaders(),
  });

  if (!response.ok) {
    throw new ApiError(`API request failed: GET ${path} returned ${response.status}`, response.status);
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
    throw new ApiError(`API request failed: POST ${path} returned ${response.status}`, response.status);
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
    throw new ApiError(`API request failed: PATCH ${path} returned ${response.status}`, response.status);
  }

  return response.json() as Promise<TResponse>;
}

export async function apiDelete(path: string): Promise<void> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'DELETE',
    headers: buildHeaders(),
  });

  if (!response.ok) {
    throw new ApiError(`API request failed: DELETE ${path} returned ${response.status}`, response.status);
  }
}
