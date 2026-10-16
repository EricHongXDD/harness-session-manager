package navigationpolicy

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Policy只绑定宿主实际启动地址，不继承JS绑定扩展来源，也不访问任何目标。
type Policy struct {
	enabled bool
	origin  string
}

func New(enabled bool, startURL string) Policy {
	origin, _ := strictOrigin(startURL)
	return Policy{enabled: enabled, origin: origin}
}

func (p Policy) Allow(target string, newWindow bool) bool {
	if !p.enabled {
		return true
	}
	if newWindow || p.origin == "" {
		return false
	}
	origin, ok := strictOrigin(target)
	return ok && origin == p.origin
}

// 页面媒体只保留现产品的重编码PNG数据，不允许以资源请求打开其它协议或外部来源。
func (p Policy) AllowResource(target string) bool {
	if !p.enabled {
		return true
	}
	if len(target) <= 4194304 && strings.HasPrefix(target, "data:image/png;base64,") {
		return true
	}
	return p.Allow(target, false)
}

func strictOrigin(raw string) (string, bool) {
	if raw == "" || len(raw) > 16384 || !utf8.ValidString(raw) || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, `\`) || strings.ContainsFunc(raw, unicode.IsControl) {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" || u.Host == "" || u.User != nil {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" && scheme != "wails" {
		return "", false
	}
	if !utf8.ValidString(u.Path) || !utf8.ValidString(u.Fragment) || strings.ContainsFunc(u.Path+u.Fragment, unicode.IsControl) {
		return "", false
	}
	query, err := url.QueryUnescape(u.RawQuery)
	if err != nil || !utf8.ValidString(query) || strings.ContainsFunc(query, unicode.IsControl) {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || strings.ContainsAny(host, "% /\\?#") {
		return "", false
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return "", false
	}
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
			return "", false
		}
		if scheme == "http" && port == "80" || scheme == "https" && port == "443" {
			port = ""
		}
	}
	if strings.Contains(host, ":") {
		if net.ParseIP(host) == nil {
			return "", false
		}
		host = "[" + host + "]"
	}
	if port != "" {
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	return scheme + "://" + host, true
}
