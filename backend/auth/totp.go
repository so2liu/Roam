// TOTP（RFC 6238）两步验证：密钥由环境变量 TTMUX_WEB_TOTP_SECRET 提供（base32）。
// 设了即启用，登录在口令之外再要一个 Authenticator 上的 6 位动态码。手写实现，无第三方依赖。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateSecret 生成随机 base32 密钥（20 字节，大写无填充）。
func GenerateSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return b32.EncodeToString(b)
}

// OtpauthURI 构建 otpauth://totp 链接（Authenticator 扫码用）。
func OtpauthURI(secret, issuer, account string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

// totpAt 计算指定 30s 时间步的 6 位码。
func totpAt(secret string, counter uint64) (string, bool) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return "", false
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(buf[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := (uint32(sum[off]&0x7f) << 24) | (uint32(sum[off+1]) << 16) | (uint32(sum[off+2]) << 8) | uint32(sum[off+3])
	return fmt.Sprintf("%06d", v%1000000), true
}

// verifyTOTP 在 ±1 个 30s 窗口内校验动态码（容忍轻微时钟漂移）。
func verifyTOTP(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	now := time.Now().Unix() / 30
	for _, d := range []int64{0, -1, 1} {
		if c, ok := totpAt(secret, uint64(now+d)); ok && subtle.ConstantTimeCompare([]byte(c), []byte(code)) == 1 {
			return true
		}
	}
	return false
}
