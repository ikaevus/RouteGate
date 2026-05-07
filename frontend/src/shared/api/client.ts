const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';

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
    throw new Error(`API request failed: GET ${path} returned ${response.status}`);
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
    throw new Error(`API request failed: POST ${path} returned ${response.status}`);
  }

  return response.json() as Promise<TResponse>;
}
