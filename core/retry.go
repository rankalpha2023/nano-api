package core

import "strings"

// IsRetryableConnErr 判断是否为可重试的连接级错误（纯函数）。
// 这些错误特征表明请求尚未被上游处理，可以安全重试。
// 从 RequestHandler 的私有方法 isRetryableConnErr 提取为公共纯函数，
// 供 engine 层的 RequestHandler 调用。
func IsRetryableConnErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "EOF"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "tls: handshake"),
		strings.Contains(msg, "dial tcp"),
		strings.Contains(msg, "connect: connection refused"):
		return true
	}
	return false
}
