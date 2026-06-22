<script lang="ts" setup>
import { computed } from 'vue';
import type { DayUsage } from '../types';
import { fmtTokens as fmt } from '../lib/format';

const props = defineProps<{ days: DayUsage[] }>();

const W = 296;
const H = 54;
const PAD = 6;
const BAR_GAP = 8;

const max = computed(() => Math.max(1, ...(props.days ?? []).map((d) => d.tokens)));
const bars = computed(() => {
  const ds = props.days ?? [];
  const n = ds.length;
  if (!n) return [];
  const slot = (W - 2 * PAD) / n;
  const bw = Math.max(slot * 0.35, 6); // thinner bars
  return ds.map((d, i) => {
    const h = (d.tokens / max.value) * (H - 2 * PAD);
    const x = PAD + i * slot + (slot - bw) / 2;
    const y = H - PAD - h;
    return { x, y, w: bw, h, date: d.date, tokens: d.tokens, isToday: d.isToday };
  });
});
</script>

<template>
  <div class="weekly-wrap">
    <div class="weekly-head"><span>近 7 天</span></div>
    <svg :viewBox="`0 0 ${W} ${H}`" class="weekly-svg">
      <rect
        v-for="(b, i) in bars"
        :key="i"
        :x="b.x.toFixed(1)"
        :y="b.y.toFixed(1)"
        :width="b.w.toFixed(1)"
        :height="b.h.toFixed(1)"
        :fill="b.isToday ? '#4dabf7' : '#3a3a48'"
        rx="2"
      >
        <title>{{ b.date }}: {{ fmt(b.tokens) }} tokens{{ b.isToday ? ' (今天)' : '' }}</title>
      </rect>
    </svg>
    <div class="weekly-labels">
      <span v-for="(b, i) in bars" :key="i" :class="{ today: b.isToday }">{{ b.date.slice(-2) }}</span>
    </div>
  </div>
</template>

<style scoped>
.weekly-wrap {
  background: #1c1c24;
  border-radius: 8px;
  padding: 5px 8px 4px;
  margin-bottom: 7px;
}
.weekly-head {
  font-size: 10px;
  color: #8a8a99;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin-bottom: 2px;
}
.weekly-svg {
  width: 100%;
  height: 48px;
  display: block;
}
.weekly-labels {
  display: flex;
  font-size: 9px;
  color: #6c6c7a;
  margin-top: 1px;
}
.weekly-labels span {
  flex: 1;
  text-align: center;
}
.weekly-labels span.today {
  color: #4dabf7;
  font-weight: 600;
}
</style>
