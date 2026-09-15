import { useMemo } from 'react'
import * as THREE from 'three'

// 齿轮参数（与仿真程序对应：扫描 10~110mm → 局部 y 0~100）
export const GEAR = {
  teeth: 24,
  rRoot: 43,
  rTip: 52,
  rTraj: 54, // 轨迹半径（齿面外侧，感应器耦合位置）
  width: 100,
  zOffset: 10, // z_mm → 局部 y 的偏移
}

// 局部坐标：z_mm → y
export function zToY(zMM: number): number {
  return zMM - GEAR.zOffset
}

// 轨迹点：极坐标 → 世界坐标
export function trajPoint(thetaDeg: number, zMM: number, r = GEAR.rTraj): [number, number, number] {
  const th = (thetaDeg * Math.PI) / 180
  return [r * Math.cos(th), zToY(zMM), r * Math.sin(th)]
}

function buildGearShape(): THREE.Shape {
  const { teeth, rRoot, rTip } = GEAR
  const step = (Math.PI * 2) / teeth
  const shape = new THREE.Shape()
  const pt = (a: number, r: number): [number, number] => [r * Math.cos(a), r * Math.sin(a)]
  // 每齿：根弧 40% → 升坡 15% → 顶弧 30% → 降坡 15%
  const first = pt(0, rRoot)
  shape.moveTo(first[0], first[1])
  for (let i = 0; i < teeth; i++) {
    const a0 = i * step
    const segs: [number, number, number][] = [
      [0.0, 0.4, rRoot],
      [0.4, 0.55, NaN], // 升坡（插值半径）
      [0.55, 0.85, rTip],
      [0.85, 1.0, NaN], // 降坡
    ]
    for (const [f0, f1, r] of segs) {
      const n = 6
      for (let k = 1; k <= n; k++) {
        const f = f0 + ((f1 - f0) * k) / n
        let rr = r
        if (Number.isNaN(rr)) {
          // 坡段：线性过渡根→顶 或 顶→根
          rr = f0 < 0.5 ? rRoot + ((rTip - rRoot) * (f - 0.4)) / 0.15 : rTip - ((rTip - rRoot) * (f - 0.85)) / 0.15
        }
        const [x, y] = pt(a0 + f * step, rr)
        shape.lineTo(x, y)
      }
    }
  }
  shape.closePath()
  return shape
}

// 齿轮毛坯（齿圈），轴向沿 Y。
export function GearBlank() {
  const geom = useMemo(() => {
    const g = new THREE.ExtrudeGeometry(buildGearShape(), {
      depth: GEAR.width,
      bevelEnabled: false,
      curveSegments: 4,
    })
    g.rotateX(-Math.PI / 2) // Z 拉伸轴 → Y
    g.translate(0, GEAR.width, 0) // y ∈ [0, width]
    return g
  }, [])
  return (
    <mesh geometry={geom} castShadow receiveShadow>
      <meshStandardMaterial color="#8a919c" metalness={0.75} roughness={0.35} />
    </mesh>
  )
}

// 感应器环：跟随播放头高度。
export function SensorRing({ y }: { y: number }) {
  return (
    <group position={[0, y, 0]}>
      <mesh rotation={[Math.PI / 2, 0, 0]}>
        <torusGeometry args={[GEAR.rTraj + 2.5, 1.1, 12, 64]} />
        <meshStandardMaterial color="#c08030" metalness={0.9} roughness={0.25} emissive="#40200" />
      </mesh>
      {/* 喷液环（略低于感应器）*/}
      <mesh rotation={[Math.PI / 2, 0, 0]} position={[0, -6, 0]}>
        <torusGeometry args={[GEAR.rTraj + 2.5, 0.6, 8, 48]} />
        <meshStandardMaterial color="#3a6ea8" metalness={0.4} roughness={0.5} transparent opacity={0.85} />
      </mesh>
    </group>
  )
}
