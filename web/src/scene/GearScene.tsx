import { Canvas } from '@react-three/fiber'
import { OrbitControls, Grid as GroundGrid } from '@react-three/drei'
import type { Annotation, Trajectory } from '../types'
import { GearBlank, GEAR } from './Gear'
import { TrajectoryScene } from './Trajectory'

interface Props {
  traj: Trajectory | null
  annotations: Annotation[]
  time: number
  showExpected: boolean
}

// 主 3D 场景：齿轮 + 轨迹 + 感应器 + 标注。
export function GearScene({ traj, annotations, time, showExpected }: Props) {
  return (
    <Canvas
      camera={{ position: [150, 90, 150], fov: 42, near: 0.5, far: 2000 }}
      shadows
      style={{ background: '#0d1117' }}
    >
      <ambientLight intensity={0.45} />
      <directionalLight position={[120, 180, 90]} intensity={1.3} castShadow />
      <directionalLight position={[-90, 60, -120]} intensity={0.35} />
      <group position={[0, -GEAR.width / 2, 0]}>
        <GearBlank />
        {traj && (
          <TrajectoryScene traj={traj} annotations={annotations} time={time} showExpected={showExpected} />
        )}
      </group>
      <GroundGrid
        args={[300, 300]}
        position={[0, -GEAR.width / 2 - 12, 0]}
        cellColor="#1c2330"
        sectionColor="#2c3a4f"
        fadeDistance={420}
      />
      <OrbitControls makeDefault target={[0, 0, 0]} maxDistance={600} minDistance={40} />
    </Canvas>
  )
}
