import { useMemo } from 'react'
import type { Annotation, Trajectory } from '../types'
import { energyDensity, energyColor } from '../energy'
import { ANNOTATION_LABEL } from '../types'

interface Props {
  traj: Trajectory
  annotations: Annotation[]
  time: number
  playing: boolean
  onSeek: (t: number) => void
  onPlayPause: () => void
}

// 底部时间轴：能量密度迷你曲线 + 标注区间条 + 播放控制。
export function Timeline({ traj, annotations, time, playing, onSeek, onPlayPause }: Props) {
  const e = useMemo(() => energyDensity(traj.t, traj.z_mm, traj.power_kw), [traj])
  const t0 = traj.t[0]
  const t1 = traj.t[traj.t.length - 1]
  const epochMs = Date.parse(traj.epoch)

  // 能量曲线 SVG 路径
  const path = useMemo(() => {
    const W = 800
    const H = 46
    const maxE = Math.max(24, ...e)
    let d = ''
    for (let i = 0; i < e.length; i++) {
      const x = ((traj.t[i] - t0) / (t1 - t0)) * W
      const y = H - (e[i] / maxE) * (H - 4)
      d += (i === 0 ? 'M' : 'L') + x.toFixed(1) + ',' + y.toFixed(1)
    }
    return d
  }, [e, traj.t, t0, t1])

  // 播放头处能量值与颜色
  const headIdx = useMemo(() => {
    let i = 0
    while (i < traj.t.length - 1 && traj.t[i] < time) i++
    return i
  }, [traj.t, time])
  const headE = e[headIdx] ?? 0
  const [r, g, b] = energyColor(headE)

  return (
    <div className="timeline">
      <div className="timeline-row">
        <button className="play-btn" onClick={onPlayPause}>{playing ? '⏸ 暂停' : '▶ 播放'}</button>
        <input
          type="range"
          min={t0}
          max={t1}
          step={0.02}
          value={time}
          onChange={(ev) => onSeek(parseFloat(ev.target.value))}
          className="seek"
        />
        <span className="clock">{time.toFixed(2)} s</span>
        <span className="energy-chip" style={{ background: `rgb(${r * 255},${g * 255},${b * 255})` }}>
          e = {headE.toFixed(1)} J/mm
        </span>
      </div>
      <div className="timeline-track">
        <svg viewBox="0 0 800 46" preserveAspectRatio="none" className="energy-svg">
          <path d={path} fill="none" stroke="#4aa3ff" strokeWidth={1.2} vectorEffect="non-scaling-stroke" />
          {annotations.map((a, i) => {
            const x0 = ((Date.parse(a.t_start) - epochMs) / 1000 - t0) / (t1 - t0)
            const x1 = ((Date.parse(a.t_end) - epochMs) / 1000 - t0) / (t1 - t0)
            return (
              <rect
                key={i}
                x={Math.max(0, x0 * 800)}
                y={0}
                width={Math.max(2, (x1 - x0) * 800)}
                height={46}
                fill={a.severity > 0.5 ? 'rgba(229,72,77,0.35)' : 'rgba(245,165,36,0.3)'}
              >
                <title>{ANNOTATION_LABEL[a.type]}</title>
              </rect>
            )
          })}
          <line
            x1={((time - t0) / (t1 - t0)) * 800}
            x2={((time - t0) / (t1 - t0)) * 800}
            y1={0}
            y2={46}
            stroke="#fff"
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
        </svg>
      </div>
    </div>
  )
}
