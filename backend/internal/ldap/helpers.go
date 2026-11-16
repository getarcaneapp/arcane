package ldap

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"
)

func normalizeDN(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil {
		return lowerASCII(strings.TrimSpace(dn))
	}
	for _, rdn := range parsed.RDNs {
		for _, attr := range rdn.Attributes {
			attr.Type = lowerASCII(attr.Type)
			attr.Value = lowerASCII(attr.Value)
		}
	}
	return parsed.String()
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func isTextID(id string) bool {
	return utf8.ValidString(id) && !strings.ContainsFunc(id, unicode.IsControl)
}
