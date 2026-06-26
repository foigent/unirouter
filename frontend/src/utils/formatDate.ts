/**
 * Unified date formatting for the admin frontend.
 * All timestamps from the API are ISO 8601 strings.
 * Output: 'YYYY-MM-DD HH:mm:ss' in local timezone.
 */
export function formatDateTime(isoStr: string | null | undefined): string {
  if (!isoStr) return '-';
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return '-';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
