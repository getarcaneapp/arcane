package utils

func DerefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
