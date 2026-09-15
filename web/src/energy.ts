// 前端能量密度计算：e = P / |dz/dt|（J/mm），与 Go 标注引擎口径一致。
export function energyDensity(t: number[], z: number[], power: number[]): number[] {
  const n = t.length
  const e = new Array<number>(n).fill(0)
  for (let i = 1; i < n - 1; i++) {
    const v = Math.abs(z[i + 1] - z[i - 1]) / (t[i + 1] - t[i - 1])
    e[i] = v > 0.1 ? power[i] / v : 0
  }
  e[0] = e[1] ?? 0
  e[n - 1] = e[n - 2] ?? 0
  return e
}

// 能量密度 → 颜色（RGB 0..1）。窗口 [13,19]：低=蓝，窗口内=绿→黄，高=红。
export function energyColor(e: number, lo = 13, hi = 19): [number, number, number] {
  if (e <= 0) return [0.25, 0.25, 0.3]
  if (e < lo) {
    const k = e / lo
    return [0.15 + 0.1 * k, 0.3 + 0.4 * k, 0.95 - 0.35 * k]
  }
  if (e <= hi) {
    const k = (e - lo) / (hi - lo)
    return [0.15 + 0.75 * k, 0.85, 0.25 - 0.1 * k]
  }
  const k = Math.min(1, (e - hi) / hi)
  return [0.9 + 0.1 * k, 0.75 - 0.6 * k, 0.15]
}
