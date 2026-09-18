package netutils

import (
	"bytes"
	"cmp"
	"net"
	"strings"
)

// CompareAddresses orders IP address strings numerically, ignoring any CIDR suffix.
// Empty values sort first and malformed values last in string order.
func CompareAddresses(a, b string) int {
	if a == "" || b == "" {
		return strings.Compare(a, b)
	}

	hostA, _, _ := strings.Cut(a, "/")
	hostB, _, _ := strings.Cut(b, "/")
	ipA := net.ParseIP(hostA)
	ipB := net.ParseIP(hostB)

	switch {
	case ipA != nil && ipB != nil:
		return bytes.Compare(ipA.To16(), ipB.To16())
	case ipA != nil:
		return -1
	case ipB != nil:
		return 1
	}
	return strings.Compare(a, b)
}

// CompareSubnets orders CIDR strings by masked network address, then prefix length.
// Empty values sort first and malformed values last in string order.
func CompareSubnets(a, b string) int {
	if a == "" || b == "" {
		return strings.Compare(a, b)
	}

	_, netA, errA := net.ParseCIDR(a)
	_, netB, errB := net.ParseCIDR(b)

	switch {
	case errA == nil && errB == nil:
		if c := bytes.Compare(netA.IP.To16(), netB.IP.To16()); c != 0 {
			return c
		}
		onesA, bitsA := netA.Mask.Size()
		onesB, bitsB := netB.Mask.Size()
		return cmp.Compare(onesA+128-bitsA, onesB+128-bitsB)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}
