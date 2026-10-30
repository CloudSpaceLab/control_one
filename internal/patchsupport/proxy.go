package patchsupport

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ProxyURL excludes credentials and configuration directives as well as URL
// components which package managers interpret differently.
func ProxyURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || strings.ContainsAny(raw, "\r\n\t\"\\ ") || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("invalid repository proxy URL")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("invalid repository proxy port")
		}
	}
	return nil
}

// SquidConfig is shared by preflight and the target. The target must still
// parse it with its installed Squid; a server without Squid cannot certify it.
func SquidConfig(hosts []string) ([]byte, error) {
	if len(hosts) == 0 || len(hosts) > 256 {
		return nil, fmt.Errorf("whitelist must contain 1 to 256 hosts")
	}
	var body strings.Builder
	body.WriteString("http_port 3128\nacl local_clients src 127.0.0.0/8 ::1\nacl SSL_ports port 443\nacl Safe_ports port 80 443\nacl CONNECT method CONNECT\nhttp_access deny !Safe_ports\nhttp_access deny CONNECT !SSL_ports\n")
	for _, h := range hosts {
		if h == "" || len(h) > 253 || strings.Trim(h, ".") == "" {
			return nil, fmt.Errorf("invalid whitelist host")
		}
		for _, r := range h {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune(".-_:", r)) {
				return nil, fmt.Errorf("invalid whitelist host")
			}
		}
		fmt.Fprintf(&body, "acl allowed_hosts dstdomain %s\n", h)
	}
	body.WriteString("http_access allow local_clients allowed_hosts\nhttp_access deny all\n")
	return []byte(body.String()), nil
}
