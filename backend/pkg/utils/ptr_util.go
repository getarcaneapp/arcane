package utils

func DerefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func StringPtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
