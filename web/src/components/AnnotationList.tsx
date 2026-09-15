import type { Annotation } from '../types'
import { ANNOTATION_LABEL } from '../types'

interface Props {
  annotations: Annotation[]
  epoch: string
  onSelect: (tSec: number) => void
}

// 标注候选列表：点击定位到对应时间。
export function AnnotationList({ annotations, epoch, onSelect }: Props) {
  const epochMs = Date.parse(epoch)
  if (annotations.length === 0) {
    return <div className="ann-empty">✓ 未检出异常候选</div>
  }
  return (
    <div className="ann-list">
      {annotations.map((a, i) => (
        <button
          key={i}
          className={`ann-item ${a.severity > 0.5 ? 'sev-high' : 'sev-mid'}`}
          onClick={() => onSelect((Date.parse(a.t_start) - epochMs) / 1000)}
        >
          <div className="ann-head">
            <span className="ann-type">{ANNOTATION_LABEL[a.type] ?? a.type}</span>
            <span className="ann-sev">{(a.severity * 100).toFixed(0)}%</span>
          </div>
          <div className="ann-msg">{a.message}</div>
          <div className="ann-time">
            {new Date(a.t_start).toLocaleTimeString('zh-CN', { hour12: false })}.
            {String(new Date(a.t_start).getMilliseconds()).padStart(3, '0')}
          </div>
        </button>
      ))}
    </div>
  )
}
