// Package dhi reads Docker Hardened Image metadata from com.docker.dhi.* image labels.
package dhi

import (
	"strings"
	"time"

	"kit.getarcane.app"

	"getarcane.dev/types/v3"
)

const (
	labelPrefix = "com.docker.dhi."
	endingSoon  = 30 * 24 * time.Hour
)

// Info returns the hardened image metadata in labels, or nil when labels carry none.
func Info(labels map[string]string, now time.Time) *types.HardenedImageInfo {
	imageName, hardened := labels[labelPrefix+"name"]
	if !hardened {
		return nil
	}

	label := func(name string) string { return strings.TrimSpace(labels[labelPrefix+name]) }
	optional := func(name string) *string {
		value, ok := labels[labelPrefix+name]
		if !ok {
			return nil
		}
		return new(strings.TrimSpace(value))
	}

	endOfLifeDate := label("date.end-of-life")
	status := types.HardenedImageSupportUnknown
	if endOfLife, err := time.Parse(time.DateOnly, endOfLifeDate); err == nil {
		today := now.UTC().Truncate(24 * time.Hour)
		switch {
		case endOfLife.Before(today):
			status = types.HardenedImageEndOfLife
		case endOfLife.Sub(today) <= endingSoon:
			status = types.HardenedImageEndingSoon
		default:
			status = types.HardenedImageSupported
		}
	}

	return &types.HardenedImageInfo{
		Shell:          optional("shell"),
		PackageManager: optional("package-manager"),
		Name:           strings.TrimSpace(imageName),
		Title:          label("title"),
		Version:        label("version"),
		Definition:     label("definition"),
		Distro:         label("distro"),
		Variant:        label("variant"),
		Flavor:         label("flavor"),
		Entitlement:    label("entitlement"),
		URL:            label("url"),
		ChainID:        label("chain-id"),
		Created:        label("created"),
		ReleaseDate:    label("date.release"),
		EndOfLifeDate:  endOfLifeDate,
		SupportStatus:  status,
		Compliance:     kit.TrimNonEmpty(strings.Split(label("compliance"), ",")),
	}
}
