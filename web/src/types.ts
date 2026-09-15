// 与 Go 复盘服务 API 对应的类型。

export interface Trajectory {
  batch_id: string
  epoch: string
  dt: number
  t: number[]
  z_mm: number[]
  theta_deg: number[]
  power_kw: number[]
  freq_khz: number[]
}

export interface Annotation {
  batch_id: string
  t_start: string
  t_end: string
  type: AnnotationType
  severity: number
  message: string
  evidence?: Record<string, number>
}

export type AnnotationType =
  | 'energy_density_step'
  | 'trajectory_gap'
  | 'homing_error'
  | 'quench_valve_delay'
  | 'emissivity_mismatch'
  | 'coil_gap_shift'
  | 'clock_drift'

export const ANNOTATION_LABEL: Record<AnnotationType, string> = {
  energy_density_step: '能量密度突变',
  trajectory_gap: '轨迹缺口',
  homing_error: '轴回零错误',
  quench_valve_delay: '喷液阀延迟',
  emissivity_mismatch: '测温发射率失配',
  coil_gap_shift: '线圈间隙变化',
  clock_drift: '采样时钟漂移',
}

export interface ReplayResult {
  batch_id: string
  scenario: string
  drift_ppm: number
  annotations: Annotation[]
}

export const SCENARIOS: { id: string; label: string; desc: string }[] = [
  { id: 'baseline', label: '基线（正常）', desc: '工艺窗口内的正常批次' },
  { id: 'homing_error', label: '轴回零错误', desc: '回零后 Z 读数偏移 +4.2mm' },
  { id: 'coil_gap', label: '线圈间隙变化', desc: '12~20s 间隙 2.0→3.5mm' },
  { id: 'emissivity', label: '测温发射率设错', desc: '设定 0.90 / 真实 0.35' },
  { id: 'quench_delay', label: '喷液阀延迟', desc: '流量确认滞后 850ms' },
  { id: 'clock_drift', label: '采样时钟漂移', desc: '采集卡时基 +2000ppm' },
]
