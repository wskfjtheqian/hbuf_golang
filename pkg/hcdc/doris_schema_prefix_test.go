package hcdc

import "testing"

// Test_TargetSchemaPrefix 验证库名前缀映射：
//   - 配置了 SchemaPrefix 时，源库名拼接前缀
//   - 前缀为空时原样返回（行为不变）
//   - 位点保留库 cdc_record 始终豁免，不参与映射
func Test_TargetSchemaPrefix(t *testing.T) {
	t.Run("有前缀", func(t *testing.T) {
		d := NewDoris(&DorisConfig{SchemaPrefix: "ods_"})
		if got := d.targetSchema("game"); got != "ods_game" {
			t.Fatalf("targetSchema(game) = %q, want ods_game", got)
		}
		if got := d.targetSchema("game_user"); got != "ods_game_user" {
			t.Fatalf("targetSchema(game_user) = %q, want ods_game_user", got)
		}
	})

	t.Run("空前缀保持原名", func(t *testing.T) {
		d := NewDoris(&DorisConfig{SchemaPrefix: ""})
		if got := d.targetSchema("game"); got != "game" {
			t.Fatalf("targetSchema(game) = %q, want game", got)
		}
	})

	t.Run("cdc_record 豁免", func(t *testing.T) {
		d := NewDoris(&DorisConfig{SchemaPrefix: "ods_"})
		if got := d.targetSchema(cdcRecordSchema); got != cdcRecordSchema {
			t.Fatalf("targetSchema(cdc_record) = %q, want cdc_record", got)
		}
	})

	t.Run("nil 配置安全", func(t *testing.T) {
		d := NewDoris(nil)
		if got := d.targetSchema("game"); got != "game" {
			t.Fatalf("targetSchema(game) with nil cfg = %q, want game", got)
		}
	})
}

// Test_DorisConfigEqualSchemaPrefix 验证 SchemaPrefix 参与配置相等性判断，
// 确保 SetConfig 在仅前缀变化时能正确触发重建。
func Test_DorisConfigEqualSchemaPrefix(t *testing.T) {
	base := &DorisConfig{Host: "h", Username: "u", Schema: "s", LoadURL: "l"}
	same := &DorisConfig{Host: "h", Username: "u", Schema: "s", LoadURL: "l"}
	diffPrefix := &DorisConfig{Host: "h", Username: "u", Schema: "s", LoadURL: "l", SchemaPrefix: "ods_"}

	if !base.Equal(same) {
		t.Fatalf("equal configs should be equal")
	}
	if base.Equal(diffPrefix) {
		t.Fatalf("configs differing only in SchemaPrefix should not be equal")
	}
}
