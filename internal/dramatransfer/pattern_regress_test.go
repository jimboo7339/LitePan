package dramatransfer

import (
	"regexp"
	"testing"

	"github.com/dlclark/regexp2"
)

// TestValidatePattern_Lookahead 回归测试：验证 (?!) 负向先行断言可以通过校验（来自Trae）。
// 该回归直接对应线上事故：用户在追剧命名规则里保存带 (?!.*纯享)(?!.*加更)... 的正则时被误判为无效。
func TestValidatePattern_Lookahead(t *testing.T) {
	pattern := `^(?!.*纯享)(?!.*加更)(?!.*抢先)(?!.*预告)(?!.*先导)(?!.*陪看)(?!.*超前).*?第\d+期.*`

	if _, err := regexp.Compile(pattern); err == nil {
		t.Fatalf("expected RE2 to reject %q", pattern)
	}
	if _, err := regexp2.Compile(pattern, 0); err != nil {
		t.Fatalf("expected regexp2 to accept %q, got %v", pattern, err)
	}
	if err := ValidatePattern(pattern); err != nil {
		t.Fatalf("ValidatePattern rejected lookahead pattern: %v", err)
	}
	flavor, err := PatternFlavor(pattern)
	if err != nil {
		t.Fatalf("PatternFlavor error: %v", err)
	}
	if flavor != "PCRE" {
		t.Fatalf("PatternFlavor = %q, want PCRE", flavor)
	}
	c := compilePattern(pattern)
	if c == nil {
		t.Fatalf("compilePattern returned nil for %q", pattern)
	}
	if c.pcre == nil {
		t.Fatalf("compilePattern did not produce PCRE branch for %q", pattern)
	}
	if ok := c.MatchString("腾讯视频 第3期 完整"); !ok {
		t.Errorf("expected match on valid title")
	}
	if ok := c.MatchString("腾讯视频 第3期 纯享版"); ok {
		t.Errorf("expected no match on 纯享 variant")
	}
}

// TestValidatePattern_BadPattern 反向用例：非法 pattern 仍应报错，避免误放行（来自Trae）。
func TestValidatePattern_BadPattern(t *testing.T) {
	if err := ValidatePattern(`([unclosed`); err == nil {
		t.Fatalf("expected error for unclosed group, got nil")
	}
}
