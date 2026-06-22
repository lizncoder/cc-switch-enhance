export namespace snapshot {
	
	export class BalanceInfo {
	    isAvailable: boolean;
	    totalBalance: string;
	    grantedBalance: string;
	    toppedUpBalance: string;
	    currency: string;
	
	    static createFrom(source: any = {}) {
	        return new BalanceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isAvailable = source["isAvailable"];
	        this.totalBalance = source["totalBalance"];
	        this.grantedBalance = source["grantedBalance"];
	        this.toppedUpBalance = source["toppedUpBalance"];
	        this.currency = source["currency"];
	    }
	}
	export class DayUsage {
	    date: string;
	    tokens: number;
	    isToday: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DayUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.date = source["date"];
	        this.tokens = source["tokens"];
	        this.isToday = source["isToday"];
	    }
	}
	export class ModelBreakdown {
	    model: string;
	    requests: number;
	    inputTokens: number;
	    outputTokens: number;
	    totalCostUsd: number;
	
	    static createFrom(source: any = {}) {
	        return new ModelBreakdown(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.requests = source["requests"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.totalCostUsd = source["totalCostUsd"];
	    }
	}
	export class ModelInfo {
	    display: string;
	    baseUrl: string;
	    baseHost: string;
	    liveModel: string;
	    match: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.display = source["display"];
	        this.baseUrl = source["baseUrl"];
	        this.baseHost = source["baseHost"];
	        this.liveModel = source["liveModel"];
	        this.match = source["match"];
	    }
	}
	export class PlanLimit {
	    window: string;
	    kind: string;
	    percent: number;
	    nextResetMs: number;
	
	    static createFrom(source: any = {}) {
	        return new PlanLimit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.window = source["window"];
	        this.kind = source["kind"];
	        this.percent = source["percent"];
	        this.nextResetMs = source["nextResetMs"];
	    }
	}
	export class ProviderInfo {
	    id: string;
	    name: string;
	    category: string;
	    websiteUrl: string;
	    costMultiplier: number;
	    limitDailyUsd?: number;
	    limitMonthlyUsd?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProviderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.websiteUrl = source["websiteUrl"];
	        this.costMultiplier = source["costMultiplier"];
	        this.limitDailyUsd = source["limitDailyUsd"];
	        this.limitMonthlyUsd = source["limitMonthlyUsd"];
	    }
	}
	export class RequestInfo {
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
	
	    static createFrom(source: any = {}) {
	        return new RequestInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.requestModel = source["requestModel"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.cacheRead = source["cacheRead"];
	        this.cacheCreate = source["cacheCreate"];
	        this.contextTokens = source["contextTokens"];
	        this.totalCostUsd = source["totalCostUsd"];
	        this.estCostUsd = source["estCostUsd"];
	        this.latencyMs = source["latencyMs"];
	        this.statusCode = source["statusCode"];
	        this.error = source["error"];
	        this.ageSec = source["ageSec"];
	    }
	}
	export class SeriesPoint {
	    t: number;
	    in: number;
	    out: number;
	
	    static createFrom(source: any = {}) {
	        return new SeriesPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.in = source["in"];
	        this.out = source["out"];
	    }
	}
	export class SessionInfo {
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
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.live = source["live"];
	        this.pid = source["pid"];
	        this.status = source["status"];
	        this.sessionId = source["sessionId"];
	        this.cwd = source["cwd"];
	        this.requests = source["requests"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.cacheRead = source["cacheRead"];
	        this.cacheCreate = source["cacheCreate"];
	        this.contextTokens = source["contextTokens"];
	        this.realCostUsd = source["realCostUsd"];
	        this.estCostUsd = source["estCostUsd"];
	        this.latestInput = source["latestInput"];
	        this.latestOutput = source["latestOutput"];
	        this.latestCacheRead = source["latestCacheRead"];
	        this.latestCacheCreate = source["latestCacheCreate"];
	        this.latestContextTokens = source["latestContextTokens"];
	        this.latestModel = source["latestModel"];
	        this.ageSec = source["ageSec"];
	    }
	}
	export class UsageTotals {
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
	
	    static createFrom(source: any = {}) {
	        return new UsageTotals(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.requests = source["requests"];
	        this.successes = source["successes"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.cacheRead = source["cacheRead"];
	        this.cacheCreate = source["cacheCreate"];
	        this.contextTokens = source["contextTokens"];
	        this.realCostUsd = source["realCostUsd"];
	        this.estCostUsd = source["estCostUsd"];
	        this.showEstCost = source["showEstCost"];
	    }
	}
	export class Snapshot {
	    appType: string;
	    availableApps: string[];
	    generatedAt: string;
	    provider: ProviderInfo;
	    model: ModelInfo;
	    today: UsageTotals;
	    month: UsageTotals;
	    session: SessionInfo;
	    latest?: RequestInfo;
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
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.appType = source["appType"];
	        this.availableApps = source["availableApps"];
	        this.generatedAt = source["generatedAt"];
	        this.provider = this.convertValues(source["provider"], ProviderInfo);
	        this.model = this.convertValues(source["model"], ModelInfo);
	        this.today = this.convertValues(source["today"], UsageTotals);
	        this.month = this.convertValues(source["month"], UsageTotals);
	        this.session = this.convertValues(source["session"], SessionInfo);
	        this.latest = this.convertValues(source["latest"], RequestInfo);
	        this.perModelToday = this.convertValues(source["perModelToday"], ModelBreakdown);
	        this.errors = source["errors"];
	        this.series = this.convertValues(source["series"], SeriesPoint);
	        this.collapsed = source["collapsed"];
	        this.todayAllAppsTokens = source["todayAllAppsTokens"];
	        this.tokens5h = source["tokens5h"];
	        this.tokens7d = source["tokens7d"];
	        this.planLimits = this.convertValues(source["planLimits"], PlanLimit);
	        this.balance = this.convertValues(source["balance"], BalanceInfo);
	        this.warn = source["warn"];
	        this.warnReason = source["warnReason"];
	        this.weeklyUsage = this.convertValues(source["weeklyUsage"], DayUsage);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

