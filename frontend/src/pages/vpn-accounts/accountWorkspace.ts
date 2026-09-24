export const accountSections = ['overview', 'access', 'routing', 'protocols', 'traffic', 'settings'] as const;
export type AccountSection = (typeof accountSections)[number];

export function isAccountSection(value?: string): value is AccountSection {
  return accountSections.some((section) => section === value);
}

export function accountWorkspaceHref(
  accountId: string,
  section: AccountSection,
  searchParams: URLSearchParams,
): string {
  const params = new URLSearchParams(searchParams);
  params.delete('create');
  params.delete('addDevice');
  const query = params.toString();
  return `/vpn-accounts/${encodeURIComponent(accountId)}/${section}${query ? `?${query}` : ''}`;
}
