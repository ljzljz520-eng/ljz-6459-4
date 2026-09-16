# 齿轮感应淬火复盘服务（replayd）

对齿轮感应淬火批次做**只读复盘**：对齐多源异时钟信号，标注能量密度突变与
轨迹缺口等工艺异常候选，辅助工程师发现"最终硬度合格但局部工艺中断"的隐患。

## 安全边界

**本服务不发送任何控制指令。** 代码库中不存在指向机床控制通道的客户端：
NATS 只订阅不发布，ClickHouse 只读写复盘数据，gRPC 仅拉取测温摘要。
机床运行完全由操作员在外部机床侧执行。

## 架构

```
 机床侧(外部)                复盘服务 (Go)
 ┌────────────┐  高频功率/轴位置 ┌─────────────────────────────┐
 │ 采集卡      │ ──────────────▶ │ ClickHouse  hf_power/hf_axis │
 │ (时钟可漂移) │                 └──────────▲──────────────────┘
 ├────────────┤  NATS 机床事件               │ 查询
 │ CNC/PLC    │ ──────────────▶ EventSource(只消费)
 ├────────────┤  gRPC GetSummary             │
 │ 红外测温仪  │ ◀────────────── PyrometerClient
 └────────────┘                              ▼
                              ┌────────────────────────────┐
                              │ pipeline.Analyze            │
                              │  1. align: 锚点→仿射时钟模型 │
                              │  2. resample: 统一时间网格   │
                              │  3. annotate: 7 类检测器     │
                              └───────────┬────────────────┘
                                          │ REST（只读）
                                          ▼
                              React Three Fiber 齿面轨迹回放
```

## 信号对齐（align）

时钟拓扑：CNC 事件时钟为参考；采集卡时钟存在固定偏移 + 线性漂移（ppm 级）。
用跨时钟域的同一物理事件做锚点（`scan_start` ↔ 功率首越 50% 设定、
`scan_end` ↔ 功率跌落），最小二乘拟合仿射模型 `t_ref = α·t_src + β`，
漂移率超阈值（默认 500ppm）即标注 `clock_drift` 并自动校正时间轴。

## 标注检测器（annotate）

| 类型 | 判据 | 回放场景 |
|---|---|---|
| `energy_density_step` | e=P/v 偏离扫描段中位数 >15% 或越出工艺窗口 | 线圈间隙 |
| `trajectory_gap` | 实际 Z 与程序期望轨迹偏差 >1.5mm 持续 >0.3s | 回零错误 |
| `homing_error` | `homing_done` 报告的 Z 读数 \|z\|>0.5mm | 回零错误 |
| `quench_valve_delay` | `quench_cmd`→`quench_flow` 滞后 > 设定+200ms | 喷液阀延迟 |
| `emissivity_mismatch` | 辐射比 (T_read⁴-T_amb⁴)/(T_pred⁴-T_amb⁴) 中位数偏离 1 >12%，并估算真实发射率 | 发射率设错 |
| `coil_gap_shift` | 频率偏移 >0.4kHz 且能量密度同步下降 >10% | 线圈间隙 |
| `clock_drift` | 对齐拟合漂移率 >500ppm | 时钟漂移 |

## 角色与流程（approval）

```
工程师 ──批准(感应器编号+程序版本+喷液配方)──▶ approved
操作员 ──扫描绑定(工件序列号+线圈编号)────────▶ bound ──(外部机床运行)
系统   ◀──NATS 事件推进──────── running → done
检验员 ──录入(硬化层深度+裂纹检查+结论)────────▶ inspected
```

### NATS 事件接线约定

subject：`machine.events.{机床号}.{事件类型}`（事件类型取末段），JSON 顶层字段：

```json
{
  "ts": "2026-09-15T08:00:05Z",
  "batch_id": "B-2026-0007",
  "source": "cnc",
  "payload": { "reported_z_mm": "4.200" }
}
```

`batch_id` 是消息**顶层字段**（对应 ClickHouse `machine_events.batch_id` 列），
适配器解析到 `domain.MachineEvent.BatchID`，驱动批次状态机；
`payload` 只承载事件自身参数（如 `homing_done` 的 `reported_z_mm`），
不得把 `batch_id` 放进 `payload`。时间戳非法或 JSON 损坏的消息直接丢弃并记录日志。

## 运行

```bash
# 纯演示模式（无外部依赖，回放场景驱动）
go run ./cmd/replayd
# 接入真实系统
CLICKHOUSE_DSN=clickhouse://localhost:9000/hardening \
NATS_URL=nats://localhost:4222 PYRO_ADDR=localhost:50051 \
go run ./cmd/replayd
```

## API 摘要

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/batches` | 创建批次 |
| POST | `/v1/batches/{id}/approve` | 工程师批准（头 `X-User`） |
| POST | `/v1/batches/{id}/bind` | 操作员扫描绑定 |
| POST | `/v1/batches/{id}/inspect` | 检验员录入结论 |
| POST | `/v1/replay/{scenario}/run` | 运行回放场景 → 标注 |
| GET  | `/v1/replay/{scenario}/trajectory?step=N` | 对齐轨迹（前端 3D） |
| GET  | `/v1/batches/{id}/annotations` | 批次标注 |

回放场景：`baseline` / `homing_error` / `coil_gap` / `emissivity` /
`quench_delay` / `clock_drift`。

## 测试

```bash
go test ./...   # 6 场景端到端 + 证据断言 + bufconn gRPC + 角色权限
```

## 前端（web/）

React Three Fiber 齿面回放：齿轮参数化建模、轨迹按能量密度着色
（蓝→绿→黄→红）、期望轨迹虚线对比、标注空间定位、感应器环随播放头移动。

```bash
cd web && npm run build   # 产物 dist/，开发 npm run dev（代理 /v1 → :8080）
```
