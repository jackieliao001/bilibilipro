package bilibili

import (
	"encoding/json"
	"testing"
)

// 预计算向量（text="abc"、key="key"，与 HMAC 链定义一致）：
//   - 规则 0/1/2 单步输出
//   - 规则 [0,1,2] 链式输出（每步输出小写 hex 作为下一步输入）
const (
	hmacMD5ABC    = "d2fe98063f876b03193afb49b4979591"
	hmacSHA1ABC   = "4fd0b215276ef12f2b3e4c8ecac2811498b656fc"
	hmacSHA256ABC = "9c196e32dc0175f86f4b1cb89289d6619de6bee699e4c378e68309ed97a1a6ab"
	hmacChain012  = "f0b18419f4f6621977de29d4412e1ebfa75e9c0e9f5913d0112ccf7e03b765f3"
)

// TestSypderRuleMapping 规则映射：0=HMAC-MD5、1=HMAC-SHA1、2=HMAC-SHA256。
func TestSypderRuleMapping(t *testing.T) {
	c := LiveHeartBeatCrypto{}
	cases := []struct {
		rule int
		want string
	}{
		{0, hmacMD5ABC},
		{1, hmacSHA1ABC},
		{2, hmacSHA256ABC},
	}
	for _, tc := range cases {
		got, err := c.Sypder("abc", []int{tc.rule}, "key")
		if err != nil {
			t.Fatalf("rule %d error: %v", tc.rule, err)
		}
		if got != tc.want {
			t.Fatalf("rule %d = %s, want %s", tc.rule, got, tc.want)
		}
	}
}

// TestSypderChain 链式计算：每个 HMAC 的输出（小写 hex）作为下一步输入。
func TestSypderChain(t *testing.T) {
	c := LiveHeartBeatCrypto{}
	got, err := c.Sypder("abc", []int{0, 1, 2}, "key")
	if err != nil {
		t.Fatalf("chain error: %v", err)
	}
	if got != hmacChain012 {
		t.Fatalf("chain = %s, want %s", got, hmacChain012)
	}
}

// TestSypderDeterministic 同输入同输出。
func TestSypderDeterministic(t *testing.T) {
	c := LiveHeartBeatCrypto{}
	a, err := c.Sypder("some-input-text", []int{0, 2, 1}, "secret")
	if err != nil {
		t.Fatalf("first error: %v", err)
	}
	b, err := c.Sypder("some-input-text", []int{0, 2, 1}, "secret")
	if err != nil {
		t.Fatalf("second error: %v", err)
	}
	if a != b {
		t.Fatalf("同输入应同输出: %s vs %s", a, b)
	}
	if a == "some-input-text" {
		t.Fatal("输出不应等于输入")
	}
}

// TestSypderUnsupportedRules 规则 3/4/5（SHA224/SHA512/SHA384）原版未实现，应报错。
func TestSypderUnsupportedRules(t *testing.T) {
	c := LiveHeartBeatCrypto{}
	for _, rule := range []int{3, 4, 5} {
		if _, err := c.Sypder("abc", []int{rule}, "key"); err == nil {
			t.Fatalf("rule %d 应返回不支持错误", rule)
		}
	}
}

// TestSypderUnknownRuleIgnored 未知规则对应原版 default: break，结果保持不变。
func TestSypderUnknownRuleIgnored(t *testing.T) {
	c := LiveHeartBeatCrypto{}
	got, err := c.Sypder("abc", []int{99, 100}, "key")
	if err != nil {
		t.Fatalf("未知规则不应报错: %v", err)
	}
	if got != "abc" {
		t.Fatalf("未知规则应保持结果不变: %s", got)
	}
}

// TestHeartBeatPayloadKeyOrder 签名 JSON 键序固定：
// platform,parent_id,area_id,seq_id,room_id,buvid,uuid,ets,time,ts。
func TestHeartBeatPayloadKeyOrder(t *testing.T) {
	p := HeartBeatPayload{
		Platform: "web", ParentID: 11, AreaID: 22, SeqID: 3, RoomID: 44,
		Buvid: "buvid", UUID: "uuid", Ets: 55, Time: 60, Ts: 66,
	}
	got, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	want := `{"platform":"web","parent_id":11,"area_id":22,"seq_id":3,"room_id":44,"buvid":"buvid","uuid":"uuid","ets":55,"time":60,"ts":66}`
	if string(got) != want {
		t.Fatalf("键序错误:\n got: %s\nwant: %s", got, want)
	}
}

// TestHeartBeatSign Sign = 固定键序 JSON 文本 + HMAC 链，结果可手工复算。
func TestHeartBeatSign(t *testing.T) {
	c := LiveHeartBeatCrypto{}
	p := HeartBeatPayload{
		Platform: "web", ParentID: 1, AreaID: 2, SeqID: 1, RoomID: 1001,
		Buvid: "buvid", UUID: "uuid", Ets: 100, Time: 60, Ts: 200,
	}
	got, err := c.Sign(p, "key", []int{0})
	if err != nil {
		t.Fatalf("Sign error: %v", err)
	}
	text, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	manual, err := c.Sypder(string(text), []int{0}, "key")
	if err != nil {
		t.Fatalf("Sypder error: %v", err)
	}
	if got != manual {
		t.Fatalf("Sign = %s, manual = %s", got, manual)
	}
	if len(got) != 32 {
		t.Fatalf("HMAC-MD5 输出应为 32 位 hex, got %d 位", len(got))
	}
}
