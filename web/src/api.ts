import type { ReplayResult, Trajectory } from './types'

export async function runReplay(scenario: string): Promise<ReplayResult> {
  const r = await fetch(`/v1/replay/${scenario}/run`, { method: 'POST' })
  if (!r.ok) throw new Error(`复盘失败: ${r.status}`)
  return r.json()
}

export async function fetchTrajectory(scenario: string, step = 4): Promise<Trajectory> {
  const r = await fetch(`/v1/replay/${scenario}/trajectory?step=${step}`)
  if (!r.ok) throw new Error(`轨迹获取失败: ${r.status}`)
  return r.json()
}
