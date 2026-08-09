package bilibili

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
)

// LiveHeartBeatCrypto 直播间心跳 HMAC 签名器，对应原版
// Ray.BiliBiliTool.Agent/BiliBiliAgent/Utils/LiveHeartBeatCrypto.Sypder。
// 纯标准库实现（crypto/hmac + md5/sha1/sha256）。
type LiveHeartBeatCrypto struct{}

// hmacHashFn 返回规则对应的 HMAC 底层哈希函数。
// 规则：0=HMAC-MD5、1=HMAC-SHA1、2=HMAC-SHA256；
// 3/4/5（HMAC-SHA224/SHA512/SHA384）原版 Hash() 未实现会抛异常，这里返回
// supported=false；其余未知规则原版 switch 走 default: break（结果保持不变），
// 返回 (nil, true) 表示跳过该规则。
func hmacHashFn(rule int) (func() hash.Hash, bool) {
	switch rule {
	case 0:
		return md5.New, true
	case 1:
		return sha1.New, true
	case 2:
		return sha256.New, true
	case 3, 4, 5:
		return nil, false
	default:
		return nil, true
	}
}

// Sypder 执行 HMAC 签名链：result 初始为 text，按规则顺序对当前 result 以
// key 为密钥计算 HMAC，输出（小写 hex 字符串）作为下一步的输入文本。
// 规则 3/4/5（SHA224/SHA512/SHA384）返回错误（与原版 Unsupported algorithm 一致）；
// 未知规则保持结果不变。
func (LiveHeartBeatCrypto) Sypder(text string, rules []int, key string) (string, error) {
	result := text
	for _, rule := range rules {
		hashFn, supported := hmacHashFn(rule)
		if !supported {
			return "", fmt.Errorf("unsupported algorithm: HMAC rule %d", rule)
		}
		if hashFn == nil {
			continue
		}
		mac := hmac.New(hashFn, []byte(key))
		mac.Write([]byte(result))
		result = hex.EncodeToString(mac.Sum(nil))
	}
	return result, nil
}

// HeartBeatPayload X25Kn/X 心跳签名载荷。
// 注意：JSON 键序固定为 platform,parent_id,area_id,seq_id,room_id,buvid,uuid,ets,time,ts
// （与原版 HeartBeatRequest 构造的匿名对象键序一致，Go 按结构体字段声明顺序序列化，
// 因此必须用 struct 而非 map）。
type HeartBeatPayload struct {
	Platform string `json:"platform"`
	ParentID int64  `json:"parent_id"`
	AreaID   int64  `json:"area_id"`
	SeqID    int    `json:"seq_id"`
	RoomID   int64  `json:"room_id"`
	Buvid    string `json:"buvid"`
	UUID     string `json:"uuid"`
	Ets      int64  `json:"ets"`
	Time     int    `json:"time"`
	Ts       int64  `json:"ts"`
}

// Sign 构造固定键序的 JSON 签名文本并执行 HMAC 链，返回 X25Kn/X 的 s 字段值。
func (c LiveHeartBeatCrypto) Sign(p HeartBeatPayload, secretKey string, rules []int) (string, error) {
	text, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("序列化心跳签名载荷失败: %w", err)
	}
	return c.Sypder(string(text), rules, secretKey)
}
