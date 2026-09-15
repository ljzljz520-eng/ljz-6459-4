// Package domain 定义齿轮感应淬火复盘的核心领域模型。
// 安全边界：本服务只读对齐与标注，绝不向机床发送控制指令。
package domain

import "time"

// Role 参与角色。权限边界：
//   - 工程师：批准感应器编号、程序版本、喷液配方
//   - 操作员：扫描工件与线圈，在外部机床运行（本系统不控制机床）
//   - 检验员：录入硬化层深度与裂纹检查结论
type Role string

const (
	RoleEngineer  Role = "engineer"
	RoleOperator  Role = "operator"
	RoleInspector Role = "inspector"
)

// Approval 工程师批准的工艺三元组。
type Approval struct {
	InductorID     string    `json:"inductor_id"`
	ProgramVersion string    `json:"program_version"`
	QuenchRecipe   string    `json:"quench_recipe"`
	ApprovedBy     string    `json:"approved_by"`
	ApprovedAt     time.Time `json:"approved_at"`
}

// ScanBinding 操作员上料扫描绑定（工件序列号 + 线圈编号）。
type ScanBinding struct {
	PartSerial string    `json:"part_serial"`
	CoilSerial string    `json:"coil_serial"`
	BoundBy    string    `json:"bound_by"`
	BoundAt    time.Time `json:"bound_at"`
}

// Inspection 检验员结论：硬化层深度与裂纹检查。
type Inspection struct {
	CaseDepthMM float64   `json:"case_depth_mm"`
	HardnessHV  float64   `json:"hardness_hv"`
	CrackFound  bool      `json:"crack_found"`
	Verdict     string    `json:"verdict"` // pass / rework / scrap
	Inspector   string    `json:"inspector"`
	InspectedAt time.Time `json:"inspected_at"`
}

// Program 工艺程序：期望轨迹与工艺窗口，标注引擎的基准。
type Program struct {
	Version        string     `json:"version"`
	ZStartMM       float64    `json:"z_start_mm"`       // 扫描起点（回零后坐标系）
	ZEndMM         float64    `json:"z_end_mm"`         // 扫描终点
	ScanSpeedMMS   float64    `json:"scan_speed_mm_s"`  // 期望扫描速度
	PowerSetKW     float64    `json:"power_set_kw"`     // 设定功率
	FreqSetKHz     float64    `json:"freq_set_khz"`     // 设定频率
	QuenchDelayMS  float64    `json:"quench_delay_ms"`  // 加热结束到喷液的设定延迟
	EnergyWindowJM [2]float64 `json:"energy_window_jm"` // 单位扫描长度能量合格窗口 [min,max] J/mm
	GapNominalMM   float64    `json:"gap_nominal_mm"`   // 感应器间隙标称值
	GapTolMM       float64    `json:"gap_tol_mm"`       // 轨迹/间隙容差
	EmissivitySet  float64    `json:"emissivity_set"`   // 红外测温发射率设定
	RotRPM         float64    `json:"rot_rpm"`          // 工件旋转转速
	TempCalibK     float64    `json:"temp_calib_k"`     // 标定升温系数 °C/kJ（标定批次回归）
	AmbientC       float64    `json:"ambient_c"`        // 环境温度
}

// PowerSample 高频功率/频率采样（采集时钟，可能漂移）。
type PowerSample struct {
	Ts      time.Time `json:"ts"`
	PowerKW float64   `json:"power_kw"`
	FreqKHz float64   `json:"freq_khz"`
}

// AxisSample 高频轴位置采样：Z 扫描轴 + 工件旋转角。
type AxisSample struct {
	Ts       time.Time `json:"ts"`
	ZMM      float64   `json:"z_mm"`
	ThetaDeg float64   `json:"theta_deg"`
}

// 机床事件类型（NATS 上行，机床时钟即参考时钟）。
const (
	EvHomingDone = "homing_done" // 回零完成，payload.reported_z_mm 为回零后读数
	EvScanStart  = "scan_start"  // 扫描开始（对齐锚点）
	EvScanEnd    = "scan_end"    // 扫描结束（对齐锚点）
	EvQuenchCmd  = "quench_cmd"  // 程序发出喷液指令
	EvQuenchFlow = "quench_flow" // 喷液流量确认（阀后流量开关）
	EvProgramEnd = "program_end"
	EvAlarm      = "alarm"
)

// MachineEvent 机床事件。
type MachineEvent struct {
	Ts      time.Time         `json:"ts"`
	Source  string            `json:"source"` // cnc / plc / scanner
	Type    string            `json:"type"`
	Payload map[string]string `json:"payload,omitempty"`
}

// PyroSummary 红外测温摘要（gRPC 拉取，测温仪时钟）。
type PyroSummary struct {
	Ts         time.Time `json:"ts"`
	Channel    string    `json:"channel"`
	TempC      float64   `json:"temp_c"`
	Emissivity float64   `json:"emissivity"` // 测温仪当时使用的发射率设定
}

// AnnotationType 标注类型（候选，需人工复核确认）。
type AnnotationType string

const (
	AnnEnergyDensityStep AnnotationType = "energy_density_step" // 能量密度突变
	AnnTrajectoryGap     AnnotationType = "trajectory_gap"      // 轨迹缺口候选
	AnnHomingError       AnnotationType = "homing_error"        // 轴回零错误
	AnnQuenchDelay       AnnotationType = "quench_valve_delay"  // 喷液阀延迟
	AnnEmissivity        AnnotationType = "emissivity_mismatch" // 测温发射率设错
	AnnCoilGapShift      AnnotationType = "coil_gap_shift"      // 线圈间隙变化
	AnnClockDrift        AnnotationType = "clock_drift"         // 高频采样时钟漂移
)

// Annotation 一条标注候选。
type Annotation struct {
	BatchID  string             `json:"batch_id"`
	TStart   time.Time          `json:"t_start"`
	TEnd     time.Time          `json:"t_end"`
	Type     AnnotationType     `json:"type"`
	Severity float64            `json:"severity"` // 0..1
	Message  string             `json:"message"`
	Evidence map[string]float64 `json:"evidence,omitempty"`
}

// BatchData 一个淬火批次的全部输入信号。
type BatchData struct {
	BatchID string
	Epoch   time.Time // 批次纪元：各时钟域 t=0 对应的墙钟

	Program Program
	Power   []PowerSample
	Axis    []AxisSample
	Events  []MachineEvent
	Pyro    []PyroSummary
}
