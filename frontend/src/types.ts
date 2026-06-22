// Mirror of the Go snapshot.Snapshot payload (see internal/snapshot/types.go).

export interface ProviderInfo {
  id: string;
  name: string;
  category: string;
  websiteUrl: string;
  costMultiplier: number;
  limitDailyUsd: number | null;
  limitMonthlyUsd: number | null;
}

export interface ModelInfo {
  display: string;
  baseUrl: string;
  baseHost: string;
  liveModel: string;
  match: boolean;
}

export interface SeriesPoint {
  t: number;
  in: number;
  out: number;
}

export interface PlanLimit {
  window: string;
  kind: string;
  percent: number;
  nextResetMs: number;
}

export interface BalanceInfo {
  isAvailable: boolean;
  totalBalance: string;
  grantedBalance: string;
  toppedUpBalance: string;
  currency: string;
}

export interface UsageTotals {
  requests: number;
  successes: number;
  inputTokens: number;
  outputTokens: number;
  cacheRead: number;
  cacheCreate: number;
  contextTokens: number;
  realCostUsd: number;
  estCostUsd: number;
  showEstCost: boolean;
}

export interface SessionInfo {
  live: boolean;
  pid: number;
  status: string;
  sessionId: string;
  cwd: string;
  requests: number;
  inputTokens: number;
  outputTokens: number;
  cacheRead: number;
  cacheCreate: number;
  contextTokens: number;
  realCostUsd: number;
  estCostUsd: number;
  latestInput: number;
  latestOutput: number;
  latestCacheRead: number;
  latestCacheCreate: number;
  latestContextTokens: number;
  latestModel: string;
  ageSec: number;
}

export interface RequestInfo {
  model: string;
  requestModel: string;
  inputTokens: number;
  outputTokens: number;
  cacheRead: number;
  cacheCreate: number;
  contextTokens: number;
  totalCostUsd: number;
  estCostUsd: number;
  latencyMs: number;
  statusCode: number;
  error: string;
  ageSec: number;
}

export interface ModelBreakdown {
  model: string;
  requests: number;
  inputTokens: number;
  outputTokens: number;
  totalCostUsd: number;
}

export interface DayUsage {
  date: string;
  tokens: number;
  isToday: boolean;
}

export interface Snapshot {
  appType: string;
  availableApps: string[];
  generatedAt: string;
  provider: ProviderInfo;
  model: ModelInfo;
  today: UsageTotals;
  month: UsageTotals;
  session: SessionInfo;
  latest: RequestInfo | null;
  perModelToday: ModelBreakdown[];
  errors: string[];
  series: SeriesPoint[];
  collapsed: boolean;
  todayAllAppsTokens: number;
  tokens5h: number;
  tokens7d: number;
  planLimits: PlanLimit[];
  balance?: BalanceInfo;
  warn: boolean;
  warnReason: string;
  weeklyUsage: DayUsage[];
}
