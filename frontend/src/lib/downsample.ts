// Evenly-spaced downsampling that always keeps the first and last samples.
// Used to keep chart/sparkline rendering cheap regardless of sample count.

export interface Sample {
  t: number;
  in: number;
  out: number;
}

export function downsample<T extends Sample>(arr: T[], max: number): T[] {
  if (max < 2 || arr.length <= max) return arr;
  const step = (arr.length - 1) / (max - 1);
  const out: T[] = [];
  for (let i = 0; i < max; i++) {
    let idx = Math.round(i * step);
    if (idx >= arr.length) idx = arr.length - 1;
    out.push(arr[idx]);
  }
  return out;
}
