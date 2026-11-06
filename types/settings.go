package types

import (
	"regexp"
)

type (
	SettingsUpdate map[string]string

	SettingUpdate struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}

	SettingState struct {
		Key string `json:"key"`

		Value   string `json:"value"`
		Type    string `json:"type"`
		Default string `json:"default"`

		Source string `json:"source" enum:"default,db,env"`

		LockReason string `json:"lockReason,omitempty" enum:"env,readOnly"`

		Scope     string `json:"scope" enum:"manager,authentication,environment"`
		EnvName   string `json:"envName"`
		Locked    bool   `json:"locked"`
		Sensitive bool   `json:"sensitive"`
	}

	SettingsRuntime struct {
		BackupVolumeName            string `json:"backupVolumeName,omitzero"`
		FrontendTracingEnabled      bool   `json:"frontendTracingEnabled"`
		FrontendMetricsEnabled      bool   `json:"frontendMetricsEnabled"`
		FrontendLogsEnabled         bool   `json:"frontendLogsEnabled"`
		AgentMTLSManagerCAAvailable bool   `json:"agentMTLSManagerCAAvailable,omitzero"`
		DepotConfigured             bool   `json:"depotConfigured,omitzero"`
	}

	SettingsSnapshot struct {
		Settings []SettingState  `json:"settings"`
		Runtime  SettingsRuntime `json:"runtime"`
		ReadOnly bool            `json:"readOnly"`
	}
)

const (
	DefaultAccentColor = "oklch(0.606 0.25 292.717)"
)

var (
	SafeThemeColor = regexp.MustCompile(`^(?:#[0-9a-fA-F]{3,8}|(?:rgb|rgba|hsl|hsla|oklch|oklab)\([0-9a-zA-Z%. ,/-]{1,64}\))$`)

	SafeAccentColor = regexp.MustCompile(
		`^(?:#[0-9a-fA-F]{3,8}|(?i:aliceblue|antiquewhite|aqua|aquamarine|azure|beige|bisque|black|blanchedalmond|blue|blueviolet|brown|burlywood|cadetblue|` +
			`chartreuse|chocolate|coral|cornflowerblue|cornsilk|crimson|cyan|darkblue|darkcyan|darkgoldenrod|darkgray|darkgreen|darkgrey|darkkhaki|darkmagenta|` +
			`darkolivegreen|darkorange|darkorchid|darkred|darksalmon|darkseagreen|darkslateblue|darkslategray|darkslategrey|darkturquoise|darkviolet|deeppink|` +
			`deepskyblue|dimgray|dimgrey|dodgerblue|firebrick|floralwhite|forestgreen|fuchsia|gainsboro|ghostwhite|gold|goldenrod|gray|green|greenyellow|grey|` +
			`honeydew|hotpink|indianred|indigo|ivory|khaki|lavender|lavenderblush|lawngreen|lemonchiffon|lightblue|lightcoral|lightcyan|lightgoldenrodyellow|` +
			`lightgray|lightgreen|lightgrey|lightpink|lightsalmon|lightseagreen|lightskyblue|lightslategray|lightslategrey|lightsteelblue|lightyellow|lime|` +
			`limegreen|linen|magenta|maroon|mediumaquamarine|mediumblue|mediumorchid|mediumpurple|mediumseagreen|mediumslateblue|mediumspringgreen|mediumturquoise|` +
			`mediumvioletred|midnightblue|mintcream|mistyrose|moccasin|navajowhite|navy|oldlace|olive|olivedrab|orange|orangered|orchid|palegoldenrod|palegreen|` +
			`paleturquoise|palevioletred|papayawhip|peachpuff|peru|pink|plum|powderblue|purple|rebeccapurple|red|rosybrown|royalblue|saddlebrown|salmon|sandybrown|` +
			`seagreen|seashell|sienna|silver|skyblue|slateblue|slategray|slategrey|snow|springgreen|steelblue|tan|teal|thistle|tomato|transparent|turquoise|violet|` +
			`wheat|white|whitesmoke|yellow|yellowgreen)|(?:rgb|rgba|hsl|hsla|oklch|oklab)\([0-9a-zA-Z%. ,/-]{1,64}\))$`,
	)
)
