const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';

export async function apiGet<TResponse>(path: string): Promise<TResponse> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    method: 'GET',
    headers: {
      Accept: 'application/json',
    },
  });

  if (!response.ok) {
    throw new Error(`API request failed: GET ${path} returned ${response.status}`);
  }

  return response.json() as Promise<TResponse>;
}
