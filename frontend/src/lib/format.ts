// Shared formatters used across the overlay UI. Previously duplicated in
// App.vue, Chart.vue and WeeklyChart.vue.

// Compact token count: 0 / 1.2k / 5.52M.
export function fmtTokens(n: number): string {
  if (!n) return '0';
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'k';
  return String(n);
}

// Age/uptime in compact form: 3m / 2h5m / 90s.
export function fmtDuration(sec: number): string {
  if (!sec || sec < 0) return '';
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (h > 0) return `${h}h${m}m`;
  if (m > 0) return `${m}m`;
  return `${sec}s`;
}

// Time remaining until an absolute epoch-ms deadline: 4h9m / 4d4h / 12m.
export function fmtRemain(ms: number): string {
  if (!ms) return '';
  let s = Math.max(0, Math.floor((ms - Date.now()) / 1000));
  if (s < 60) return s + 'm';
  if (s < 3600) return Math.floor(s / 60) + 'm';
  if (s < 86400) {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    return h + 'h' + (m ? m + 'm' : '');
  }
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  return d + 'd' + (h ? h + 'h' : '');
}
