package types

import (
	"regexp"
)

// SettingsUpdate maps setting keys to their new string values.
type SettingsUpdate map[string]string

// SettingState describes one setting and how its effective value was resolved.
type SettingState struct {
	// Key is the identifier of the setting.
	//
	// Required: true
	Key string `json:"key"`

	// Value is the effective setting value. Sensitive values are masked.
	//
	// Required: true
	Value string `json:"value"`

	// Type is the data type of the setting value.
	//
	// Required: true
	Type string `json:"type"`

	// Default is the built-in default value.
	//
	// Required: true
	Default string `json:"default"`

	// Source is where the effective value comes from: default, db, or env.
	//
	// Required: true
	Source string `json:"source" enum:"default,db,env"`

	// Locked indicates the setting cannot be changed through the UI, API, or CLI.
	//
	// Required: true
	Locked bool `json:"locked"`

	// LockReason explains why the setting is locked: env or readOnly. Empty when unlocked.
	//
	// Required: false
	LockReason string `json:"lockReason,omitempty" enum:"env,readOnly"`

	// Sensitive indicates the value is a secret and is masked in responses.
	//
	// Required: true
	Sensitive bool `json:"sensitive"`

	// Scope is manager for settings only the main environment owns, otherwise environment.
	//
	// Required: true
	Scope string `json:"scope" enum:"manager,environment"`

	// EnvName is the environment variable that sets the setting.
	//
	// Required: true
	EnvName string `json:"envName"`
}

// SettingsRuntime carries runtime values derived from server configuration.
type SettingsRuntime struct {
	// FrontendTracingEnabled indicates the frontend may relay traces.
	//
	// Required: true
	FrontendTracingEnabled bool `json:"frontendTracingEnabled"`

	// FrontendMetricsEnabled indicates the frontend may relay metrics.
	//
	// Required: true
	FrontendMetricsEnabled bool `json:"frontendMetricsEnabled"`

	// FrontendLogsEnabled indicates the frontend may relay logs.
	//
	// Required: true
	FrontendLogsEnabled bool `json:"frontendLogsEnabled"`

	// BackupVolumeName is the Docker volume that stores backups.
	//
	// Required: false
	BackupVolumeName string `json:"backupVolumeName,omitzero"`

	// EdgeMTLSManagerCAAvailable indicates the manager mTLS CA can be downloaded.
	//
	// Required: false
	EdgeMTLSManagerCAAvailable bool `json:"edgeMTLSManagerCAAvailable,omitzero"`

	// DepotConfigured indicates both the Depot project ID and token are set.
	//
	// Required: false
	DepotConfigured bool `json:"depotConfigured,omitzero"`
}

// SettingsSnapshot is the settings state of one environment.
type SettingsSnapshot struct {
	// Settings lists every setting visible to the caller.
	//
	// Required: true
	Settings []SettingState `json:"settings"`

	// Runtime carries values derived from server configuration.
	//
	// Required: true
	Runtime SettingsRuntime `json:"runtime"`

	// ReadOnly indicates UI configuration is disabled and every setting is locked.
	//
	// Required: true
	ReadOnly bool `json:"readOnly"`
}

// DefaultAccentColor is the fallback accent color used when no override is
// provided or the configured value fails validation.
const DefaultAccentColor = "oklch(0.606 0.25 292.717)"

// SafeAccentColor accepts only safe CSS color tokens: hex, known named colors,
// rgb()/rgba(), hsl()/hsla(), oklch()/oklab(). Values containing semicolons,
// angle brackets, or quotes are rejected to prevent CSS/SVG injection when
// the value is substituted into a style declaration.
var SafeAccentColor = regexp.MustCompile(
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
