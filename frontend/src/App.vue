<script lang="ts" setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue';
import { EventsOn, EventsOff, WindowSetSize } from '../wailsjs/runtime/runtime';
import { GetSnapshot, ListApps, OpenCCSwitch, CCSwitchInstalled, SetApp, SetCollapsed, SetBarWidth, SetCardHeight, GetAutoStart, SetAutoStart } from '../wailsjs/go/main/App';
import ccSwitchIcon from './assets/images/cc-switch.png';
import type { Snapshot, SeriesPoint } from './types';
import Chart from './components/Chart.vue';
import WeeklyChart from './components/WeeklyChart.vue';
import { fmtTokens, fmtDuration, fmtRemain } from './lib/format';

const snap = ref<Snapshot | null>(null);
const apps = ref<string[]>([]);
// Whether cc-switch is installed; the launcher icon shows only when true.
const ccSwitchInstalled = ref(false);
// Whether cc-enhance starts automatically at user login.
const autoStart = ref(false);

const dotClass = computed(() => {
  const s = snap.value;
  if (!s || !s.session.live) return 'dot gray'; // 闲置：灰色
  return 'dot green';                            // 正在使用：绿色
});
// "Model working" — drives the breathing border when the LLM is actively busy.
const working = computed(() => !!snap.value?.session?.live && snap.value?.session?.status === 'busy');
const series = computed<SeriesPoint[]>(() => snap.value?.series ?? []);
// Total consumed tokens for the selected app = full context + output.
const todayTotal = computed(() => {
  const t = snap.value?.today;
  return t ? t.contextTokens + t.outputTokens : 0;
});
const sessionLabel = computed(() => {
  const s = snap.value?.session;
  if (!s || !s.live) return '空闲';
  return (s.status === 'busy' ? '忙' : '闲') + (s.ageSec ? ' · ' + fmtDuration(s.ageSec) : '');
});
// Refresh clock ticks on every event receipt (independent of payload), and the
// signature gate (in the snapshot handler) skips re-rendering when the usage
// data is unchanged — together the clock stays alive while idle ticks avoid the
// full reactive re-evaluation + chart redraw.
const lastRefreshMs = ref(0);
let lastDataSig = '';
const refreshTime = computed(() => lastRefreshMs.value ? new Date(lastRefreshMs.value).toLocaleTimeString('zh-CN', { hour12: false }) : '');
// Plan quota windows: 5小时 and 7天 token limits (labels match cc switch).
const limit5h = computed(() => (snap.value?.planLimits ?? []).find((p) => p.window === '5小时') ?? null);
const limit7d = computed(() => (snap.value?.planLimits ?? []).find((p) => p.window === '7天') ?? null);
const pct5h = computed(() => limit5h.value?.percent ?? null);
const pct7d = computed(() => limit7d.value?.percent ?? null);
const remain5h = computed(() => (limit5h.value ? fmtRemain(limit5h.value.nextResetMs) : ''));
const remain7d = computed(() => (limit7d.value ? fmtRemain(limit7d.value.nextResetMs) : ''));

// GLM (智谱) exposes time-window quota percentages; DeepSeek only has balance.
// UI differs per billing model: GLM shows 5h/7d %, DeepSeek shows balance only.
const isGLM = computed(() => !!(snap.value?.planLimits && snap.value.planLimits.length));
const stat5h = computed(() => pct5h.value != null ? pct5h.value + '%' + (remain5h.value ? ' · ' + remain5h.value : '') : '—');
const stat7d = computed(() => pct7d.value != null ? pct7d.value + '%' + (remain7d.value ? ' · ' + remain7d.value : '') : '—');

// OpenCode mode: aggregated relay, show per-model breakdown.
const isOpenCode = computed(() => !!snap.value?.model?.baseHost?.toLowerCase().includes('opencode.ai'));
// perModel sorted by requests, memoized on a content signature so idle ticks
// (identical model breakdown) don't re-sort every 1.5s. A fresh array is
// returned only when the signature changes, so Vue's computed equality check
// short-circuits dependents on unchanged ticks.
let perModelSig = '';
let perModelCache: { model: string; requests: number; inputTokens: number; outputTokens: number }[] = [];
const perModel = computed(() => {
  const src = snap.value?.perModelToday ?? [];
  const sig = src.length + ':' + src.map((m) => m.model + '#' + m.requests + '#' + m.inputTokens + '#' + m.outputTokens).join(',');
  if (sig === perModelSig) return perModelCache;
  perModelSig = sig;
  perModelCache = src.slice().sort((a, b) => b.requests - a.requests);
  return perModelCache;
});
const perModelTotal = computed(() => perModel.value.reduce((s, m) => ({ requests: s.requests + m.requests, tokens: s.tokens + m.inputTokens + m.outputTokens }), { requests: 0, tokens: 0 }));
// Collapsed bar: cycle one model at a time through all available models.
const modelIdx = ref(0);
const currentModel = computed(() => perModel.value[modelIdx.value] ?? null);
const modelCycleText = computed(() => {
  const m = currentModel.value;
  return m ? m.model + ' ' + m.requests + '次' : '';
});
let modelTimer = 0;
function startModelCycle() {
  stopModelCycle();
  modelTimer = window.setInterval(() => {
    if (perModel.value.length) modelIdx.value = (modelIdx.value + 1) % perModel.value.length;
  }, 2500);
}
function stopModelCycle() { if (modelTimer) { clearInterval(modelTimer); modelTimer = 0; } }
onMounted(() => { /* interval started after load below */ });
// Start cycle when perModel data changes; cleanup on unmount.
// Only restart when model list length changes (prevent resetting every 1.5s snap).
let lastCycleN = 0;
watch(perModel, (arr) => {
  if (arr.length && arr.length !== lastCycleN) {
    lastCycleN = arr.length;
    modelIdx.value = 0;
    startModelCycle();
  }
}, { immediate: true });

// Auto-fit the collapsed bar to its content width.
const barRef = ref<HTMLElement | null>(null);
const cardRef = ref<HTMLElement | null>(null);
const barInOut = computed(() => {
  const s = snap.value?.session;
  if (!s) return '';
  return '▲' + fmtTokens(s.latestContextTokens) + ' ▼' + fmtTokens(s.latestOutput);
});
// Collapsed-bar vertical carousel items (one shown at a time, cycling).
const barItems = computed<string[]>(() => {
  const items: string[] = [];
  if (isOpenCode.value) {
    // OpenCode aggregated plan: cycle per-model request counts (no balance).
    for (const m of perModel.value) items.push(m.model + ' ' + m.requests + '次');
    return items;
  }
  if (barInOut.value) items.push(barInOut.value);
  const bal = snap.value?.balance;
  if (bal) items.push('余额 ' + bal.totalBalance + ' ' + bal.currency);
  if (isGLM.value) {
    items.push('5h ' + stat5h.value);
    items.push('7天 ' + stat7d.value);
  }
  return items;
});
const barIdx = ref(0);
const barCurrent = computed(() => barItems.value[barIdx.value] ?? '');
let barTimer = 0;
let lastBarN = 0;
function startBarCycle() {
  stopBarCycle();
  barTimer = window.setInterval(() => {
    if (barItems.value.length > 1) barIdx.value = (barIdx.value + 1) % barItems.value.length;
  }, 4000);
}
function stopBarCycle() { if (barTimer) { clearInterval(barTimer); barTimer = 0; } }
watch(barItems, (arr) => {
  if (arr.length > 1 && arr.length !== lastBarN) { lastBarN = arr.length; barIdx.value = 0; startBarCycle(); }
  else if (arr.length <= 1) { stopBarCycle(); }
}, { immediate: true });
function fitBar() {
  nextTick(() => {
    // Measure content width via max-content
    const el = barRef.value;
    if (!el) return;
    const prev = el.style.width;
    el.style.width = 'max-content';
    const measured = Math.ceil(el.getBoundingClientRect().width);
    el.style.width = prev;
    const w = Math.min(Math.max(measured + 6, 280), 760);
    // Re-assert the size on every call. Go-side WindowSetSize is unreliable for
    // this frameless always-on-top window, so this JS call is the authoritative
    // sizer — without it the height drifts back to the expanded value after a
    // collapse. (Frequency is already bounded by the snapshot diff guard, which
    // skips this entirely on idle ticks.)
    WindowSetSize(w, 36);
  });
}
// Re-fit when the bar collapses or the model name / cycled metric changes.
watch([() => snap.value?.collapsed, () => snap.value?.model?.display, barCurrent], () => {
  if (snap.value?.collapsed) fitBar();
});
// Quota warning: red pulse on <body>. Takes priority over the green "working"
// border — when warning, drop the working class so red wins.
const warn = computed(() => !!snap.value?.warn);
watch([working, warn], ([w, wa]) => {
  document.body.classList.toggle('working', !!w && !wa);
  document.body.classList.toggle('warn', !!wa);
}, { immediate: true });

let lastCardH = 0;

async function loadInitial() {
  try {
    const s = (await GetSnapshot()) as unknown as Snapshot;
    lastRefreshMs.value = Date.now();
    lastDataSig = JSON.stringify({ ...s, generatedAt: '' });
    snap.value = s;
    apps.value = (await ListApps()) as unknown as string[];
    ccSwitchInstalled.value = await CCSwitchInstalled();
    autoStart.value = await GetAutoStart();
    fitCard(s);
  } catch (e) { console.error('initial load failed', e); }
}
async function selectApp(app: string) {
  if (app === snap.value?.appType) return;
  try { await SetApp(app); } catch (e) { console.error('set app failed', e); }
}
async function collapse(on: boolean) {
  try { await SetCollapsed(on); } catch (e) { console.error('collapse failed', e); }
}
// Launch (or focus) the cc-switch app so the user can switch providers without
// leaving the overlay. Best-effort: the backend probes common install paths +
// Start Menu shortcuts and logs if cc-switch isn't installed.
async function openCCSwitch() {
  try { await OpenCCSwitch(); } catch (e) { console.error('open cc-switch failed', e); }
}
async function toggleAutoStart() {
  const next = !autoStart.value;
  try { await SetAutoStart(next); autoStart.value = next; } catch (e) { console.error('set auto-start failed', e); }
}

let off = false;
function fitCard(snapVal: Snapshot | null) {
  if (!snapVal || snapVal.collapsed) return;
  nextTick(() => {
    const el = cardRef.value;
    if (!el) return;
    const h = Math.ceil(el.scrollHeight + 4);
    if (h !== lastCardH) { lastCardH = h; SetCardHeight(h); }
  });
}
onMounted(() => {
  loadInitial();
  EventsOn('snapshot', (payload: unknown) => {
    lastRefreshMs.value = Date.now();
    const p = payload as Snapshot;
    // Layout fit runs EVERY tick. The frameless window's height can drift after
    // a collapse/expand toggle (Go-side WindowSetSize is unreliable for it), so
    // fitBar/fitCard re-assert the correct size constantly — without this the
    // window gets stuck at the wrong height while the `collapsed` flag disagrees.
    // These are cheap (fitCard caches + Go no-ops once a manual height is set;
    // fitBar is one IPC + a DOM measure).
    if (p?.collapsed) fitBar();
    else fitCard(p);
    // Diff guard: skip the reactive re-assignment — and the chart/SVG recompute
    // and ~22 computeds it triggers — when only the refresh clock changed. This
    // is the expensive part; layout above still runs so the size stays correct.
    const sig = JSON.stringify({ ...p, generatedAt: '' });
    if (sig === lastDataSig) return;
    lastDataSig = sig;
    snap.value = p;
    if (p?.availableApps?.length) apps.value = p.availableApps;
  });
});
onBeforeUnmount(() => { if (!off) { EventsOff('snapshot'); off = true; } stopModelCycle(); stopBarCycle(); });
</script>

<template>
  <!-- ===== Collapsed one-line bar (draggable) ===== -->
  <div class="bar drag" ref="barRef" :class="{ working }" v-if="snap && snap.collapsed" @dblclick="collapse(false)">
    <span :class="dotClass"></span>
    <span class="b-model">{{ snap.model.display || '—' }}</span>
    <span class="b-cycle" :class="{ red: warn }">{{ barCurrent }}</span>
    <button class="expand" title="展开" aria-label="展开" @click.stop="collapse(false)">
      <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="15 3 21 3 21 9"/><polyline points="9 21 3 21 3 15"/><line x1="21" y1="3" x2="14" y2="10"/><line x1="3" y1="21" x2="10" y2="14"/></svg>
    </button>
  </div>

  <!-- ===== Full card ===== -->
  <div class="card" :class="{ working }" ref="cardRef" v-else-if="snap">
    <div class="topline drag" :title="snap.provider.name" tabindex="0" role="button" aria-label="双击或回车折叠" @dblclick="collapse(true)" @keydown.enter="collapse(true)" @keydown.space.prevent="collapse(true)">
      <span :class="dotClass"></span>
      <span class="model">{{ snap.model.display || '—' }}</span>
      <div class="apps">
        <button v-for="a in apps" :key="a" class="pill" :class="{ active: a === snap.appType }" @click="selectApp(a)">{{ a }}</button>
      </div>
      <div class="actions">
        <button class="icon-btn" v-if="ccSwitchInstalled" title="打开 cc-switch" aria-label="打开 cc-switch" @click="openCCSwitch">
          <img :src="ccSwitchIcon" alt="cc-switch" class="cc-switch-ico" draggable="false" />
        </button>
        <button class="collapse-btn" title="折叠为长条" aria-label="折叠为长条" @click="collapse(true)">
          <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 14 10 14 10 20"/><polyline points="20 10 14 10 14 4"/><line x1="14" y1="10" x2="21" y2="3"/><line x1="3" y1="21" x2="10" y2="14"/></svg>
        </button>
      </div>
    </div>

    <!-- Today: big consumed-total + all-apps + chart -->
    <section class="today">
      <div class="today-head">
        <span>今日消耗 · {{ snap.provider.name }}</span>
        <span class="all">全部 {{ fmtTokens(snap.todayAllAppsTokens) }}</span>
      </div>
      <div class="today-big">{{ fmtTokens(todayTotal) }}<small> tokens</small></div>
      <Chart :points="series" />
      <WeeklyChart :days="snap.weeklyUsage ?? []" />
    </section>

    <!-- Per-model breakdown (OpenCode aggregated relay) -->
    <section class="permodel" v-if="isOpenCode && perModel.length">
      <div class="permodel-head">各模型用量 · 今日</div>
      <div class="permodel-row" v-for="m in perModel" :key="m.model">
        <span class="pm-name">{{ m.model }}</span>
        <span class="pm-reqs">{{ m.requests }} 次</span>
        <span class="pm-tokens">{{ fmtTokens(m.inputTokens + m.outputTokens) }} tokens</span>
      </div>
      <div class="permodel-divider"></div>
      <div class="permodel-row permodel-total">
        <span class="pm-name">合计</span>
        <span class="pm-reqs">{{ perModelTotal.requests }} 次</span>
        <span class="pm-tokens">{{ fmtTokens(perModelTotal.tokens) }} tokens</span>
      </div>
    </section>

    <!-- Live: latest in/out + session state, one line -->
    <div class="live" aria-live="polite">
      <span class="muted">实时</span>
      <b class="in">▲{{ fmtTokens(snap.session.latestContextTokens) }}</b>
      <b class="out">▼{{ fmtTokens(snap.session.latestOutput) }}</b>
      <span class="muted">{{ sessionLabel }}</span>
    </div>

    <!-- Windowed consumption + refresh time -->
    <div class="stats">
      <span v-if="isGLM" :class="{ red: warn }"><i>5h</i>{{ stat5h }}</span>
      <span v-if="isGLM" :class="{ red: warn }"><i>7天</i>{{ stat7d }}</span>
      <label class="autostart-switch" title="开机自启" @click="toggleAutoStart">
        <span class="switch-track" :class="{ on: autoStart }">
          <span class="switch-thumb"></span>
        </span>
        <span class="switch-label">自启</span>
      </label>
      <span class="refresh">刷新 {{ refreshTime }}</span>
    </div>

    <!-- Plan quota windows (% + next reset time) -->
    <section class="plan" v-if="snap.planLimits && snap.planLimits.length">
      <div class="block-head"><span>套餐额度</span></div>
      <div class="plan-row" v-for="(p, i) in snap.planLimits" :key="i">
        <span class="plan-name">{{ p.kind }} · {{ p.window }}</span>
        <div class="plan-bar"><div class="plan-fill" :style="{ width: p.percent + '%' }"></div></div>
        <b class="plan-pct" :class="{ red: warn }">{{ p.percent }}%</b>
        <span class="plan-reset">{{ fmtRemain(p.nextResetMs) }}</span>
      </div>
    </section>

    <!-- Account balance (DeepSeek etc. — no time-window quota) -->
    <section class="plan" v-if="snap.balance && !isOpenCode && (!snap.planLimits || !snap.planLimits.length)">
      <div class="block-head"><span>账户余额</span></div>
      <div class="plan-row">
        <span class="plan-name">可用</span>
        <div class="plan-bar"><div class="plan-fill" style="width:100%" :class="{ low: !snap.balance.isAvailable }"></div></div>
        <b class="plan-pct" :class="{ red: warn }">{{ snap.balance.totalBalance }}</b>
        <span class="plan-reset">{{ snap.balance.currency }}</span>
      </div>
      <div class="plan-row" v-if="snap.balance.grantedBalance !== '0'">
        <span class="plan-name">赠送</span>
        <span class="plan-bar" style="color:#8a8a99;font-size:10px">{{ snap.balance.grantedBalance }}</span>
      </div>
      <div class="plan-row" v-if="snap.balance.toppedUpBalance !== '0'">
        <span class="plan-name">充值</span>
        <span class="plan-bar" style="color:#8a8a99;font-size:10px">{{ snap.balance.toppedUpBalance }}</span>
      </div>
    </section>

    <div class="errors" v-if="snap.errors && snap.errors.length">
      <span v-for="(e, i) in snap.errors" :key="i" class="err-chip" role="alert">⚠ {{ e }}</span>
    </div>
  </div>
  <div class="card loading" v-else>加载中…</div>
</template>

<style scoped>
.card { width: 100%; height: 100%; box-sizing: border-box; padding: 8px 10px; color: #e6e6ea; font-size: 12px; font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif; overflow: hidden; }

/* Breathing border is applied to <body> (see style.css) so the glow spans the
   full window edge — toggled via the `working` watch below. */

/* Wails reads --wails-draggable (configured in main.go), not -webkit-app-region. */
.drag { --wails-draggable: drag; }
.pill, .collapse-btn, .expand, .icon-btn, .actions { --wails-draggable: no-drag; }

.topline { display: flex; align-items: center; gap: 6px; margin-bottom: 9px; cursor: default; }
.model { font-size: 13px; font-weight: 600; color: #c8d0ff; white-space: nowrap; }
.apps { display: flex; gap: 4px; flex-wrap: wrap; margin-left: 2px; }
.pill { border: 1px solid #333; background: #1e1e26; color: #b8b8c4; border-radius: 999px; padding: 1px 8px; font-size: 10px; cursor: pointer; }
.pill.active { background: #3b5bdb; color: #fff; border-color: #3b5bdb; }
.collapse-btn, .icon-btn { border: 1px solid #333; background: #1e1e26; color: #b8b8c4; border-radius: 6px; width: 22px; height: 20px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer; transition: color .12s, background .12s, border-color .12s; }
.actions { margin-left: auto; display: flex; align-items: center; gap: 6px; }
.collapse-btn:hover, .icon-btn:hover { color: #e6e6ea; background: #2a2a36; border-color: #3a3a48; }
.cc-switch-ico { width: 14px; height: 14px; display: block; }

.today { background: #1c1c24; border-radius: 10px; padding: 9px 11px 4px; margin-bottom: 8px; }
.today-head { display: flex; justify-content: space-between; align-items: center; font-size: 10px; color: #8a8a99; text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 1px; }
.today-head .all { color: #9aa0b5; text-transform: none; letter-spacing: 0; }
.today-big { font-size: 28px; font-weight: 700; color: #fff; font-variant-numeric: tabular-nums; line-height: 1.1; margin-bottom: 4px; }
.today-big small { font-size: 11px; font-weight: 400; color: #8a8a99; margin-left: 3px; }

.permodel { background: #1c1c24; border-radius: 10px; padding: 8px 11px; margin-bottom: 8px; }
.permodel-head { font-size: 10px; color: #8a8a99; text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 6px; }
.permodel-row { display: flex; align-items: center; gap: 6px; padding: 3px 0; font-size: 11px; font-variant-numeric: tabular-nums; }
.permodel-total { border-top: none; margin-top: 2px; padding-top: 4px; font-weight: 600; color: #c8d0ff; }
.permodel-divider { height: 1px; background: #2a2a34; margin: 3px 0; }
.pm-name { width: 100px; flex: 0 0 100px; color: #e6e6ea; }
.pm-reqs { width: 50px; text-align: right; color: #9aa0b5; }
.pm-tokens { margin-left: auto; text-align: right; color: #c8d0ff; }

.live { display: flex; align-items: center; gap: 10px; padding: 0 2px; font-variant-numeric: tabular-nums; }
.live b { font-weight: 600; }
.live .in { color: #4dabf7; }
.live .out { color: #37d67a; }
.muted { color: #7a7a8a; }
.red { color: #f55050 !important; }
.red em { color: #f55050 !important; }

.stats { display: flex; align-items: center; gap: 14px; padding: 6px 2px 0; font-variant-numeric: tabular-nums; font-size: 11px; color: #c8d0ff; }
.stats span { font-weight: 600; }
.stats i { font-style: normal; font-weight: 400; color: #6c6c7a; margin-right: 4px; }
.stats .refresh { margin-left: auto; color: #6c6c7a; font-weight: 400; }

/* Auto-start toggle switch */
.autostart-switch { display: inline-flex; align-items: center; gap: 4px; cursor: pointer; user-select: none; }
.switch-track { width: 24px; height: 12px; border-radius: 6px; background: #333; position: relative; transition: background .15s; }
.switch-track.on { background: #37d67a; }
.switch-thumb { width: 8px; height: 8px; border-radius: 50%; background: #b8b8c4; position: absolute; top: 2px; left: 2px; transition: transform .15s, background .15s; }
.switch-track.on .switch-thumb { transform: translateX(12px); background: #fff; }
.switch-label { font-weight: 400; color: #6c6c7a; font-size: 10px; }

.plan { background: #1c1c24; border-radius: 8px; padding: 6px 10px 7px; margin-top: 7px; }
.plan .block-head { margin-bottom: 4px; }
.plan-row { display: flex; align-items: center; gap: 7px; padding: 2px 0; font-size: 11px; }
.plan-name { width: 64px; color: #9aa0b5; flex: 0 0 64px; }
.plan-bar { flex: 1; height: 5px; background: #2a2a34; border-radius: 3px; overflow: hidden; }
.plan-fill { height: 100%; background: linear-gradient(90deg, #3b5bdb, #4dabf7); }
.plan-fill.low { background: linear-gradient(90deg, #f5a623, #f5a623); }
.plan-pct { width: 30px; text-align: right; font-variant-numeric: tabular-nums; color: #c8d0ff; }
.plan-reset { width: 92px; text-align: right; font-size: 10px; color: #6c6c7a; font-variant-numeric: tabular-nums; }

.dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; flex: 0 0 8px; }
.dot.green { background: #37d67a; box-shadow: 0 0 6px #37d67a; }
.dot.amber { background: #f5a623; box-shadow: 0 0 6px #f5a623; }
.dot.gray { background: #555; }
.errors { margin-top: 6px; display: flex; flex-direction: column; gap: 3px; }
.err-chip { font-size: 10px; color: #ffb088; background: #2a2018; padding: 2px 6px; border-radius: 4px; }
.loading { display: flex; align-items: center; justify-content: center; color: #888; }

.bar { width: 100%; max-width: 760px; height: 100%; box-sizing: border-box; padding: 0 6px; display: flex; align-items: center; gap: 5px; color: #e6e6ea; font-size: 10px; font-family: -apple-system, "Segoe UI", "Microsoft YaHei", sans-serif; overflow-x: auto; overflow-y: hidden; cursor: default; }
.bar .b-model { font-weight: 600; color: #c8d0ff; }
.bar .b-cycle { flex: 1; text-align: right; padding-right: 12px; font-variant-numeric: tabular-nums; white-space: nowrap; color: #e6e6ea; animation: bar-slide 0.4s ease; }
@keyframes bar-slide {
  0%   { opacity: 0; transform: translateY(8px); }
  100% { opacity: 1; transform: translateY(0); }
}
.bar .expand { margin-left: auto; border: none; background: #2a2a34; color: #b8b8c4; border-radius: 4px; width: 20px; height: 20px; display: inline-flex; align-items: center; justify-content: center; cursor: pointer; flex: 0 0 auto; transition: color .12s, background .12s; }
.bar .expand:hover { color: #e6e6ea; background: #33333f; }
</style>
