import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchTrajectory, runReplay } from './api'
import type { Annotation, Trajectory } from './types'
import { SCENARIOS } from './types'
import { GearScene } from './scene/GearScene'
import { Timeline } from './components/Timeline'
import { AnnotationList } from './components/AnnotationList'

export default function App() {
  const [scenario, setScenario] = useState('baseline')
  const [traj, setTraj] = useState<Trajectory | null>(null)
  const [annotations, setAnnotations] = useState<Annotation[]>([])
  const [driftPpm, setDriftPpm] = useState(0)
  const [time, setTime] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [showExpected, setShowExpected] = useState(true)
  const [error, setError] = useState('')
  const rafRef = useRef(0)
  const lastRef = useRef(0)

  const load = useCallback(async (sc: string) => {
    setError('')
    setPlaying(false)
    try {
      const [t, r] = await Promise.all([fetchTrajectory(sc), runReplay(sc)])
      setTraj(t)
      setAnnotations(r.annotations ?? [])
      setDriftPpm(r.drift_ppm)
      setTime(t.t[0])
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    void load(scenario)
  }, [scenario, load])

  // 播放循环
  useEffect(() => {
    if (!playing || !traj) return
    lastRef.current = performance.now()
    const tick = (now: number) => {
      const dt = (now - lastRef.current) / 1000
      lastRef.current = now
      setTime((prev) => {
        const end = traj.t[traj.t.length - 1]
        const next = prev + dt
        return next > end ? traj.t[0] : next
      })
      rafRef.current = requestAnimationFrame(tick)
    }
    rafRef.current = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(rafRef.current)
  }, [playing, traj])

  return (
    <div className="app">
      <aside className="sidebar">
        <h1>齿轮感应淬火复盘</h1>
        <div className="section-title">回放场景</div>
        <div className="scenario-list">
          {SCENARIOS.map((s) => (
            <button
              key={s.id}
              className={`scenario ${s.id === scenario ? 'active' : ''}`}
              onClick={() => setScenario(s.id)}
            >
              <div className="scenario-label">{s.label}</div>
              <div className="scenario-desc">{s.desc}</div>
            </button>
          ))}
        </div>
        <div className="section-title">
          标注候选
          {Math.abs(driftPpm) > 1 && (
            <span className="drift-chip" title="对齐引擎估计的采样时钟漂移">
              漂移 {driftPpm.toFixed(0)} ppm
            </span>
          )}
        </div>
        {traj && (
          <AnnotationList annotations={annotations} epoch={traj.epoch} onSelect={(t) => setTime(t)} />
        )}
        <label className="toggle">
          <input type="checkbox" checked={showExpected} onChange={(e) => setShowExpected(e.target.checked)} />
          显示程序期望轨迹
        </label>
        {error && <div className="error">{error}</div>}
        <div className="hint">只读复盘 · 不发送控制指令</div>
      </aside>
      <main className="viewport">
        <GearScene traj={traj} annotations={annotations} time={time} showExpected={showExpected} />
        {traj && (
          <Timeline
            traj={traj}
            annotations={annotations}
            time={time}
            playing={playing}
            onSeek={setTime}
            onPlayPause={() => setPlaying((p) => !p)}
          />
        )}
      </main>
    </div>
  )
}
