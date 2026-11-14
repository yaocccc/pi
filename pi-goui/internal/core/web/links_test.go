package web

import (
	"bytes"
	"net"
	"net/url"
	"strings"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
)

func TestLoginLink(t *testing.T) {
	const token = "space +/=?&#中文"
	for _, tt := range []struct {
		name, bound, zone, lan, scheme, host string
		local                                bool
	}{
		{"IPv4 wildcard", "0.0.0.0", "", "192.168.4.20", "http", "192.168.4.20:41923", false},
		{"IPv6 wildcard", "::", "", "10.1.2.3", "https", "10.1.2.3:41923", false},
		{"concrete bind", "192.168.8.9", "", "10.1.2.3", "http", "192.168.8.9:41923", false},
		{"loopback unchanged", "127.0.0.1", "", "192.168.4.20", "http", "127.0.0.1:41923", true},
		{"IPv6 concrete", "fd00::123", "", "10.1.2.3", "https", "[fd00::123]:41923", false},
		{"IPv6 scoped", "fe80::123", "eth0", "10.1.2.3", "http", "[fe80::123%eth0]:41923", false},
		{"no LAN", "0.0.0.0", "", "", "http", "127.0.0.1:41923", true},
		{"no IPv6 LAN", "::", "", "", "http", "[::1]:41923", true},
		{"nil bound", "", "", "192.168.4.20", "http", "192.168.4.20:41923", false},
		{"reject link local", "0.0.0.0", "", "169.254.1.2", "http", "127.0.0.1:41923", true},
		{"reject public candidate", "0.0.0.0", "", "8.8.8.8", "http", "127.0.0.1:41923", true},
		{"reject IPv6 candidate", "0.0.0.0", "", "fd00::1", "http", "127.0.0.1:41923", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			link, local := loginLink(&net.TCPAddr{IP: net.ParseIP(tt.bound), Port: 41923, Zone: tt.zone}, tt.scheme, token, net.ParseIP(tt.lan))
			u, err := url.Parse(link)
			if err != nil || u.Host != tt.host || u.Scheme != tt.scheme || u.Path != "/" || u.Query().Get("pk") != token || u.Fragment != "" || local != tt.local {
				t.Fatalf("link=%q parsed=%+v local=%v err=%v", link, u, local, err)
			}
		})
	}
}

func TestPrintLoginQRCode(t *testing.T) {
	address := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 43210}
	var out, warnings bytes.Buffer
	PrintLoginQRCode(&out, &warnings, address, "http", "escaped +&token")
	link, _ := loginLink(address, "http", "escaped +&token", nil)
	if !strings.HasPrefix(out.String(), "Login URL (contains token): "+link+"\n") || !strings.Contains(warnings.String(), "--listen :8888") || !strings.Contains(warnings.String(), "Do not share") {
		t.Fatalf("missing URL or credential/loopback warning: %q, %q", out.String(), warnings.String())
	}
	_, rendering, ok := strings.Cut(out.String(), "\n")
	if !ok || !strings.HasSuffix(rendering, "\x1b[0m\n") {
		t.Fatal("missing explicit black-on-white QR rendering")
	}
	code, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		t.Fatal(err)
	}
	bitmap := code.Bitmap()
	rows := strings.Split(strings.TrimSuffix(rendering, "\n"), "\n")
	if len(rows) != (len(bitmap)+1)/2 {
		t.Fatal("QR height does not match encoder bitmap")
	}
	for row, line := range rows {
		if !strings.HasPrefix(line, "\x1b[30;47m") || !strings.HasSuffix(line, "\x1b[0m") {
			t.Fatal("each QR row must reset its background before newline")
		}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "\x1b[30;47m"), "\x1b[0m")
		pixels := []rune(line)
		if len(pixels) != len(bitmap[0]) {
			t.Fatal("QR row width does not match encoder bitmap")
		}
		for col, glyph := range pixels {
			var upper, lower bool
			switch glyph {
			case ' ':
			case '█':
				upper, lower = true, true
			case '▀':
				upper = true
			case '▄':
				lower = true
			default:
				t.Fatalf("unexpected QR glyph %q", glyph)
			}
			if upper != bitmap[row*2][col] || (row*2+1 < len(bitmap) && lower != bitmap[row*2+1][col]) {
				t.Fatalf("rendered QR module differs at %d,%d", col, row*2)
			}
		}
	}
	for y := range bitmap {
		for x, dark := range bitmap[y] {
			if (x < 4 || y < 4 || x >= len(bitmap)-4 || y >= len(bitmap)-4) && dark {
				t.Fatal("QR lacks its four-module quiet zone")
			}
		}
	}
}

func TestOversizeTokenKeepsLoginURL(t *testing.T) {
	var out, warnings bytes.Buffer
	PrintLoginQRCode(&out, &warnings, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8888}, "http", strings.Repeat("x", 4000))
	if !strings.Contains(out.String(), "?pk=") || !strings.Contains(warnings.String(), "Login QR unavailable") || strings.Contains(out.String(), "\x1b[30;47m") {
		t.Fatal("an unencodable QR must retain the login URL and explain the limitation")
	}
}
