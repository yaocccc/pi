package web

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Prefer the first active interface (by OS index), then its lowest private
// IPv4. This is deterministic without contacting an external network service.
func localLANIPv4() net.IP {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Index < interfaces[j].Index })
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		var candidates []net.IP
		for _, address := range addresses {
			var ip net.IP
			switch address := address.(type) {
			case *net.IPNet:
				ip = address.IP
			case *net.IPAddr:
				ip = address.IP
			}
			if privateIPv4(ip) {
				candidates = append(candidates, ip.To4())
			}
		}
		if len(candidates) > 0 {
			sort.Slice(candidates, func(i, j int) bool { return bytes.Compare(candidates[i], candidates[j]) < 0 })
			return candidates[0]
		}
	}
	return nil
}

func privateIPv4(ip net.IP) bool {
	return ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func loginLink(address *net.TCPAddr, scheme, token string, lanIP net.IP) (string, bool) {
	ip, zone := address.IP, address.Zone
	if len(ip) == 0 || ip.IsUnspecified() {
		zone = ""
		if privateIPv4(lanIP) {
			ip = lanIP
		} else if len(ip) != 0 && ip.To4() == nil {
			ip = net.IPv6loopback
		} else {
			ip = net.IPv4(127, 0, 0, 1)
		}
	}
	host := ip.String()
	if zone != "" {
		host += "%" + zone
	}
	link := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(address.Port)), Path: "/", RawQuery: url.Values{"pk": {token}}.Encode()}
	return link.String(), ip.IsLoopback()
}

// PrintLoginQRCode prints a credential-bearing login URL and terminal QR code.
func PrintLoginQRCode(out, errOut io.Writer, address *net.TCPAddr, scheme, token string) {
	var lanIP net.IP
	if len(address.IP) == 0 || address.IP.IsUnspecified() {
		lanIP = localLANIPv4()
	}
	link, localOnly := loginLink(address, scheme, token, lanIP)
	fmt.Fprintf(out, "Login URL (contains token): %s\n", link)
	fmt.Fprintln(errOut, "WARNING: the login URL and QR code grant access to this service. Do not share them with untrusted people.")
	if localOnly {
		if address.IP.IsLoopback() {
			fmt.Fprintln(errOut, "Local-only listener: for phone/LAN access, restart with --listen :8888.")
		} else {
			fmt.Fprintln(errOut, "No private LAN IPv4 address found; the QR code uses loopback. Use this server's reachable IP instead.")
		}
	}
	code, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		fmt.Fprintf(errOut, "Login QR unavailable: %v\n", err)
		return
	}
	// Explicit black-on-white colors make the quiet zone scannable on both
	// light and dark terminals. Keep the encoder's default four-module border.
	var rendering strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(code.ToSmallString(true), "\n"), "\n") {
		// Reset before each newline: an active background can otherwise paint
		// the entire terminal row (and blank screen below), not just the QR.
		fmt.Fprintf(&rendering, "\x1b[30;47m%s\x1b[0m\n", line)
	}
	fmt.Fprint(out, rendering.String())
}
