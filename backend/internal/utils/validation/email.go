package validation

import (
	"net"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

var localPartPattern = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+/=?^_` + "`" + `{|}~.-]+$`)

func IsValidUserEmail(email string) bool {
	trimmedEmail := strings.TrimSpace(email)
	if trimmedEmail == "" || strings.Contains(trimmedEmail, " ") {
		return false
	}

	localPart, domainPart, ok := strings.Cut(trimmedEmail, "@")
	if !ok || strings.Contains(domainPart, "@") {
		return false
	}

	return isValidLocalPartInternal(localPart) && isValidDomainPartInternal(domainPart)
}

func isValidLocalPartInternal(localPart string) bool {
	if localPart == "" || len(localPart) > 64 {
		return false
	}

	if strings.HasPrefix(localPart, ".") || strings.HasSuffix(localPart, ".") || strings.Contains(localPart, "..") {
		return false
	}

	return localPartPattern.MatchString(localPart)
}

func isValidDomainPartInternal(domainPart string) bool {
	if domainPart == "" || len(domainPart) > 255 {
		return false
	}

	if ip := net.ParseIP(domainPart); ip != nil && ip.To4() != nil {
		return true
	}

	if strings.HasPrefix(domainPart, "[") && strings.HasSuffix(domainPart, "]") {
		return isValidAddressLiteralInternal(domainPart)
	}

	asciiDomain, err := idna.Lookup.ToASCII(domainPart)
	if err != nil || asciiDomain == "" {
		return false
	}

	labels := strings.Split(asciiDomain, ".")
	if len(labels) == 4 {
		allNumeric := true
		for _, label := range labels {
			if label == "" || strings.Trim(label, "0123456789") != "" {
				allNumeric = false
				break
			}
		}
		if allNumeric {
			return false
		}
	}

	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
	}

	return true
}

func isValidAddressLiteralInternal(domainPart string) bool {
	literal := strings.TrimSuffix(strings.TrimPrefix(domainPart, "["), "]")
	if strings.HasPrefix(strings.ToLower(literal), "ipv6:") {
		ip := net.ParseIP(literal[5:])
		return ip != nil && ip.To4() == nil
	}

	ip := net.ParseIP(literal)
	return ip != nil && ip.To4() != nil && !strings.Contains(literal, ":")
}
