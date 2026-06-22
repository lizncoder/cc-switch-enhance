<script lang="ts" setup>
import { computed } from 'vue';
import { downsample } from '../lib/downsample';
import { fmtTokens as fmt } from '../lib/format';

interface P { t: number; in: number; out: number; }
const props = defineProps<{ points: P[] }>();

const W = 296;
const H = 64;
const PAD = 4;

const sampled = computed(() => downsample(props.points ?? [], 120));
const inMax = computed(() => Math.max(1, ...sampled.value.map((p) => p.in)));
const outMax = computed(() => Math.max(1, ...sampled.value.map((p) => p.out)));
const last = computed(() => (sampled.value.length ? sampled.value[sampled.value.length - 1] : null));

function xy(val: number, max: number, i: number, n: number): [number, number] {
  const x = n <= 1 ? W / 2 : PAD + (i / (n - 1)) * (W - 2 * PAD);
  const y = H - PAD - (val / max) * (H - 2 * PAD);
  return [x, y];
}

const inArea = computed(() => {
  const pts = sampled.value;
  const n = pts.length;
  if (!n) return '';
  let d = `M ${PAD},${H - PAD} `;
  pts.forEach((p, i) => {
    const [x, y] = xy(p.in, inMax.value, i, n);
    d += `L ${x.toFixed(1)},${y.toFixed(1)} `;
  });
  d += `L ${W - PAD},${H - PAD} Z`;
  return d;
});

const outLine = computed(() => {
  const pts = sampled.value;
  const n = pts.length;
  if (n < 2) return '';
  return pts
    .map((p, i) => {
      const [x, y] = xy(p.out, outMax.value, i, n);
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
});
</script>

<template>
  <div class="chart-wrap">
    <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" class="chart-svg">
      <path :d="inArea" fill="rgba(77,171,247,0.18)" stroke="rgba(77,171,247,0.5)" stroke-width="1" />
      <path :d="outLine" fill="none" stroke="#37d67a" stroke-width="1.5" stroke-linejoin="round" />
    </svg>
    <div class="legend">
      <span class="lg in">入 {{ fmt(last?.in ?? 0) }}</span>
      <span class="lg out">出 {{ fmt(last?.out ?? 0) }}</span>
      <span class="lg win">近60分</span>
    </div>
    <div class="empty" v-if="!sampled.length">近 60 分钟无请求</div>
  </div>
</template>

<style scoped>
.chart-wrap {
  position: relative;
  background: #1c1c24;
  border-radius: 8px;
  padding: 6px 8px 4px;
  margin-bottom: 7px;
}
.chart-svg {
  width: 100%;
  height: 64px;
  display: block;
}
.legend {
  display: flex;
  gap: 10px;
  font-size: 10px;
  margin-top: 2px;
}
.lg.in { color: #4dabf7; }
.lg.out { color: #37d67a; }
.lg.win { color: #6c6c7a; margin-left: auto; }
.empty {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #6c6c7a;
  font-size: 11px;
}
</style>
