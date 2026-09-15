// Package approval 实现角色分离的批次生命周期：
//
//	工程师 → 批准（感应器编号 + 程序版本 + 喷液配方）
//	操作员 → 扫描绑定（工件序列号 + 线圈编号）后在外部机床运行
//	检验员 → 录入硬化层深度与裂纹检查结论
//
// 状态机：draft → approved → bound → running → done → inspected。
// 本服务不持有任何机床控制通道，running 状态仅由机床事件推进。
package approval

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/example/gear-hardening-replay/internal/domain"
)

// Status 批次状态。
type Status string

const (
	StatusDraft     Status = "draft"
	StatusApproved  Status = "approved"
	StatusBound     Status = "bound"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusInspected Status = "inspected"
)

// Batch 批次聚合。
type Batch struct {
	ID         string
	Approval   *domain.Approval
	Binding    *domain.ScanBinding
	Inspection *domain.Inspection
	Status     Status
	CreatedAt  time.Time
}

var (
	ErrRole       = errors.New("角色无权执行该操作")
	ErrState      = errors.New("批次状态不允许该操作")
	ErrNotFound   = errors.New("批次不存在")
	ErrIncomplete = errors.New("必填字段不完整")
)

// Service 批次生命周期服务（内存实现；生产可替换为 Postgres 仓储）。
type Service struct {
	mu      sync.RWMutex
	batches map[string]*Batch
	roles   map[string]domain.Role // userID → role
	now     func() time.Time
}

// New 创建服务。roles 为用户角色目录（生产对接 IAM/LDAP）。
func New(roles map[string]domain.Role) *Service {
	return &Service{
		batches: make(map[string]*Batch),
		roles:   roles,
		now:     time.Now,
	}
}

// Create 创建批次（任何登录用户可建草稿）。
func (s *Service) Create(id string) (*Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.batches[id]; ok {
		return nil, fmt.Errorf("批次 %s 已存在", id)
	}
	b := &Batch{ID: id, Status: StatusDraft, CreatedAt: s.now()}
	s.batches[id] = b
	return b, nil
}

// Approve 工程师批准工艺三元组：感应器编号、程序版本、喷液配方。
func (s *Service) Approve(batchID, user string, a domain.Approval) error {
	if err := s.requireRole(user, domain.RoleEngineer); err != nil {
		return err
	}
	if a.InductorID == "" || a.ProgramVersion == "" || a.QuenchRecipe == "" {
		return fmt.Errorf("%w: 感应器编号/程序版本/喷液配方均必填", ErrIncomplete)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.batches[batchID]
	if !ok {
		return ErrNotFound
	}
	if b.Status != StatusDraft {
		return fmt.Errorf("%w: 当前 %s，仅 draft 可批准", ErrState, b.Status)
	}
	a.ApprovedBy = user
	a.ApprovedAt = s.now()
	b.Approval = &a
	b.Status = StatusApproved
	return nil
}

// Bind 操作员扫描绑定工件与线圈（条码/RFID）。
func (s *Service) Bind(batchID, user string, bind domain.ScanBinding) error {
	if err := s.requireRole(user, domain.RoleOperator); err != nil {
		return err
	}
	if bind.PartSerial == "" || bind.CoilSerial == "" {
		return fmt.Errorf("%w: 工件序列号与线圈编号均必填", ErrIncomplete)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.batches[batchID]
	if !ok {
		return ErrNotFound
	}
	if b.Status != StatusApproved {
		return fmt.Errorf("%w: 当前 %s，需先经工程师批准", ErrState, b.Status)
	}
	bind.BoundBy = user
	bind.BoundAt = s.now()
	b.Binding = &bind
	b.Status = StatusBound
	return nil
}

// MarkRunning 由机床事件推进：scan_start → running，program_end → done。
// 仅系统内部调用（事件消费者），不暴露给 API 用户。
func (s *Service) MarkRunning(batchID string) error {
	return s.transition(batchID, StatusBound, StatusRunning)
}

// MarkDone 程序结束。
func (s *Service) MarkDone(batchID string) error {
	return s.transition(batchID, StatusRunning, StatusDone)
}

func (s *Service) transition(batchID string, from, to Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.batches[batchID]
	if !ok {
		return ErrNotFound
	}
	if b.Status != from {
		return fmt.Errorf("%w: 当前 %s，期望 %s", ErrState, b.Status, from)
	}
	b.Status = to
	return nil
}

// Inspect 检验员录入硬化层深度与裂纹检查结论。
func (s *Service) Inspect(batchID, user string, insp domain.Inspection) error {
	if err := s.requireRole(user, domain.RoleInspector); err != nil {
		return err
	}
	if insp.CaseDepthMM <= 0 {
		return fmt.Errorf("%w: 硬化层深度必须为正", ErrIncomplete)
	}
	switch insp.Verdict {
	case "pass", "rework", "scrap":
	default:
		return fmt.Errorf("%w: verdict 须为 pass/rework/scrap", ErrIncomplete)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.batches[batchID]
	if !ok {
		return ErrNotFound
	}
	if b.Status != StatusDone && b.Status != StatusInspected {
		return fmt.Errorf("%w: 当前 %s，需程序结束后方可检验", ErrState, b.Status)
	}
	insp.Inspector = user
	insp.InspectedAt = s.now()
	b.Inspection = &insp
	b.Status = StatusInspected
	return nil
}

// Get 查询批次。
func (s *Service) Get(batchID string) (*Batch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.batches[batchID]
	if !ok {
		return nil, ErrNotFound
	}
	return b, nil
}

func (s *Service) requireRole(user string, want domain.Role) error {
	r, ok := s.roles[user]
	if !ok || r != want {
		return fmt.Errorf("%w: 用户 %s 需要角色 %s", ErrRole, user, want)
	}
	return nil
}
