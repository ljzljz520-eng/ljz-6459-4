package approval_test

import (
	"testing"

	"github.com/example/gear-hardening-replay/internal/approval"
	"github.com/example/gear-hardening-replay/internal/domain"
)

func svc() *approval.Service {
	return approval.New(map[string]domain.Role{
		"eng1": domain.RoleEngineer,
		"op1":  domain.RoleOperator,
		"qa1":  domain.RoleInspector,
	})
}

func TestLifecycle(t *testing.T) {
	s := svc()
	if _, err := s.Create("B1"); err != nil {
		t.Fatal(err)
	}
	// 操作员不能批准
	err := s.Approve("B1", "op1", domain.Approval{InductorID: "IND-7", ProgramVersion: "IH-42.3", QuenchRecipe: "QR-PAG-8%"})
	if err == nil {
		t.Error("操作员不应能批准")
	}
	// 工程师批准（缺字段被拒）
	if err = s.Approve("B1", "eng1", domain.Approval{InductorID: "IND-7"}); err == nil {
		t.Error("缺字段应被拒绝")
	}
	if err = s.Approve("B1", "eng1", domain.Approval{InductorID: "IND-7", ProgramVersion: "IH-42.3", QuenchRecipe: "QR-PAG-8%"}); err != nil {
		t.Fatal(err)
	}
	// 未绑定前不能 running
	if err = s.MarkRunning("B1"); err == nil {
		t.Error("未绑定不应进入 running")
	}
	// 操作员扫描绑定
	if err = s.Bind("B1", "op1", domain.ScanBinding{PartSerial: "GR-88001", CoilSerial: "COIL-31"}); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkRunning("B1"); err != nil {
		t.Fatal(err)
	}
	// 检验员不能在 running 时录入
	if err = s.Inspect("B1", "qa1", domain.Inspection{CaseDepthMM: 1.8, HardnessHV: 720, Verdict: "pass"}); err == nil {
		t.Error("running 状态不应允许检验")
	}
	if err = s.MarkDone("B1"); err != nil {
		t.Fatal(err)
	}
	// 工程师不能录入检验结论
	if err = s.Inspect("B1", "eng1", domain.Inspection{CaseDepthMM: 1.8, HardnessHV: 720, Verdict: "pass"}); err == nil {
		t.Error("工程师不应能录入检验结论")
	}
	if err = s.Inspect("B1", "qa1", domain.Inspection{CaseDepthMM: 1.8, HardnessHV: 720, Verdict: "pass"}); err != nil {
		t.Fatal(err)
	}
	b, _ := s.Get("B1")
	if b.Status != approval.StatusInspected {
		t.Errorf("终态应为 inspected，实际 %s", b.Status)
	}
	if b.Inspection == nil || b.Inspection.CaseDepthMM != 1.8 {
		t.Error("检验结论未落库")
	}
}
