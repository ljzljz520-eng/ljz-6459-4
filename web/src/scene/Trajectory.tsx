import { useMemo } from 'react'
import { Line, Html } from '@react-three/drei'
import type { Annotation, Trajectory } from '../types'
import { ANNOTATION_LABEL } from '../types'
import { energyColor, energyDensity } from '../energy'
import { trajPoint, zToY, SensorRing } from './Gear'

interface Props {
  traj: Trajectory
  annotations: Annotation[]
  time: number // 当前播放时间（秒，相对轨迹 t）
  showExpected: boolean
}

// 实际轨迹线：顶点色按能量密度映射。
export function TrajectoryLine({ traj }: { traj: Trajectory }) {
  const { points, colors } = useMemo(() => {
    const e = energyDensity(traj.t, traj.z_mm, traj.power_kw)
    const points: [number, number, number][] = []
    const colors: [number, number, number][] = []
    for (let i = 0; i < traj.t.length; i++) {
      points.push(trajPoint(traj.theta_deg[i], traj.z_mm[i]))
      colors.push(energyColor(e[i]))
    }
    return { points, colors }
  }, [traj])
  return <Line points={points} vertexColors={colors} lineWidth={3} />
}

// 程序期望轨迹（虚线）：回零错误等场景下与实际轨迹对比。
export function ExpectedLine({ traj }: { traj: Trajectory }) {
  const points = useMemo(() => {
    // 期望：scan_start(5s) 起 10mm → 25s 到 110mm，速度 5mm/s
    const pts: [number, number, number][] = []
    for (let i = 0; i < traj.t.length; i++) {
      const t = traj.t[i]
      const z = t < 5 ? 10 : t > 25 ? 110 : 10 + 5 * (t - 5)
      pts.push(trajPoint(traj.theta_deg[i], z, 50.5))
    }
    return pts
  }, [traj])
  return (
    <Line points={points} color="#4aa3ff" lineWidth={1.2} dashed dashSize={2} gapSize={1.5} transparent opacity={0.6} />
  )
}

// 播放头：当前时刻在轨迹上的位置。
export function Playhead({ traj, time }: { traj: Trajectory; time: number }) {
  const pos = useMemo(() => {
    const idx = nearestIndex(traj.t, time)
    return trajPoint(traj.theta_deg[idx], traj.z_mm[idx])
  }, [traj, time])
  return (
    <mesh position={pos}>
      <sphereGeometry args={[1.6, 16, 16]} />
      <meshStandardMaterial color="#ffffff" emissive="#ffffff" emissiveIntensity={0.6} />
    </mesh>
  )
}

function nearestIndex(t: number[], time: number): number {
  let lo = 0
  let hi = t.length - 1
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (t[mid] < time) lo = mid + 1
    else hi = mid
  }
  return lo
}

// 标注标记：异常时间段在轨迹上的空间定位。
export function AnnotationMarkers({ traj, annotations }: { traj: Trajectory; annotations: Annotation[] }) {
  const epochMs = useMemo(() => Date.parse(traj.epoch), [traj.epoch])
  const markers = useMemo(() => {
    return annotations.map((a, k) => {
      const tSec = (Date.parse(a.t_start) - epochMs) / 1000
      const idx = nearestIndex(traj.t, Math.max(traj.t[0], Math.min(tSec, traj.t[traj.t.length - 1])))
      return {
        key: k,
        pos: trajPoint(traj.theta_deg[idx], traj.z_mm[idx], 58),
        label: ANNOTATION_LABEL[a.type] ?? a.type,
        severity: a.severity,
      }
    })
  }, [annotations, traj, epochMs])
  return (
    <>
      {markers.map((m) => (
        <group key={m.key} position={m.pos}>
          <mesh>
            <octahedronGeometry args={[2.2]} />
            <meshStandardMaterial
              color={m.severity > 0.5 ? '#e5484d' : '#f5a524'}
              emissive={m.severity > 0.5 ? '#e5484d' : '#f5a524'}
              emissiveIntensity={0.5}
            />
          </mesh>
          <Html distanceFactor={180} style={{ pointerEvents: 'none' }}>
            <div className="marker-label">{m.label}</div>
          </Html>
        </group>
      ))}
    </>
  )
}

// 场景组装。
export function TrajectoryScene({ traj, annotations, time, showExpected }: Props) {
  const headY = useMemo(() => {
    const idx = nearestIndex(traj.t, time)
    return zToY(traj.z_mm[idx])
  }, [traj, time])
  return (
    <>
      <TrajectoryLine traj={traj} />
      {showExpected && <ExpectedLine traj={traj} />}
      <Playhead traj={traj} time={time} />
      <AnnotationMarkers traj={traj} annotations={annotations} />
      <SensorRingAt y={headY} />
    </>
  )
}

function SensorRingAt({ y }: { y: number }) {
  return <SensorRing y={y} />
}
