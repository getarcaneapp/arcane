package cmdutil

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestEnumValue(t *testing.T) {
	type mode string
	for _, tt := range []struct {
		name  string
		input string
		want  mode
		valid bool
	}{
		{name: "allowed", input: "json", want: "json", valid: true},
		{name: "invalid", input: "xml", want: "text"},
		{name: "case insensitive", input: "JSON", want: "json", valid: true},
		{name: "whitespace", input: " json ", want: "text"},
		{name: "empty", input: "", want: "", valid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := mode("text")
			value := &EnumValue[mode]{value: &current, allowed: []string{"text", "json"}}
			setErr := value.Set(tt.input)
			if tt.valid {
				require.NoError(t, setErr)
			} else {
				require.ErrorContains(t, setErr, "allowed: text, json")
			}
			require.Equal(t, tt.want, current)
			require.Equal(t, string(tt.want), value.String())
			require.Equal(t, "string", value.Type())
		})
	}
}

func TestEnumVarP(t *testing.T) {
	type mode string
	for _, tt := range []struct {
		name         string
		input        string
		persistent   bool
		valid        bool
		short        bool
		emptyDefault bool
	}{
		{name: "local valid", input: "json", valid: true},
		{name: "local invalid", input: "xml"},
		{name: "persistent valid", persistent: true, input: "json", valid: true},
		{name: "persistent invalid", persistent: true, input: "xml"},
		{name: "local shorthand", input: "json", valid: true, short: true},
		{name: "persistent shorthand", persistent: true, input: "json", valid: true, short: true},
		{name: "optional default", input: "json", valid: true, emptyDefault: true},
		{name: "optional empty", input: "", valid: true, emptyDefault: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := &cobra.Command{Use: "root"}
			child := &cobra.Command{Use: "child"}
			root.AddCommand(child)
			owner, flags := child, child.Flags()
			if tt.persistent {
				owner, flags = root, root.PersistentFlags()
			}
			var current mode
			allowed := []mode{"text", "json"}
			defaultValue := mode("text")
			if tt.emptyDefault {
				defaultValue = ""
			}
			EnumVarP(owner, flags, &current, "mode", "m", defaultValue, "Output mode", allowed...)
			require.Equal(t, "Output mode (text, json)", flags.Lookup("mode").Usage)
			require.Equal(t, defaultValue, current)
			allowed[0] = "changed"
			completion, ok := owner.GetFlagCompletionFunc("mode")
			require.True(t, ok)
			choices, directive := completion(child, nil, "")
			require.Equal(t, []string{"text", "json"}, choices)
			require.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

			flagName := "--mode"
			if tt.short {
				flagName = "-m"
			}
			parseErr := child.ParseFlags([]string{flagName, tt.input})
			if tt.valid {
				require.NoError(t, parseErr)
				require.Equal(t, mode(tt.input), current)
			} else {
				require.ErrorContains(t, parseErr, "allowed: text, json")
				require.Equal(t, defaultValue, current)
			}
			got, getErr := child.Flags().GetString("mode")
			require.NoError(t, getErr)
			require.Equal(t, string(current), got)
		})
	}
}

func TestEnumVarPInvalidDefault(t *testing.T) {
	for _, tt := range []struct {
		name    string
		allowed []string
	}{
		{name: "empty choices"},
		{name: "invalid default", allowed: []string{"text", "json"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "root"}
			current := "unchanged"
			require.PanicsWithValue(t, `flag --mode: default invalid value "xml" (allowed: `+strings.Join(tt.allowed, ", ")+`)`, func() {
				EnumVarP(cmd, cmd.Flags(), &current, "mode", "", "xml", "Output mode", tt.allowed...)
			})
			require.Equal(t, "unchanged", current)
			require.Nil(t, cmd.Flags().Lookup("mode"))
		})
	}
}

func TestExclusiveFlags(t *testing.T) {
	for _, tt := range []struct {
		name    string
		wantErr string
		args    []string
	}{
		{name: "defaults do not conflict"},
		{name: "one boolean", args: []string{"--inuse"}},
		{name: "explicit false", args: []string{"--inuse=false", "--unused"}},
		{name: "empty string", args: []string{"--filter=", "--unused"}},
		{name: "zero integer", args: []string{"--limit=0", "--unused"}},
		{name: "empty slice", args: []string{"--labels=", "--unused"}},
		{name: "boolean conflict", args: []string{"--inuse", "--unused"}, wantErr: "flags --inuse and --unused cannot be used together"},
		{name: "string conflict", args: []string{"--filter=0", "--unused"}, wantErr: "flags --unused and --filter cannot be used together"},
		{name: "integer conflict", args: []string{"--limit=1", "--unused"}, wantErr: "flags --unused and --limit cannot be used together"},
		{name: "slice conflict", args: []string{"--labels=app", "--unused"}, wantErr: "flags --unused and --labels cannot be used together"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{
				Use: "list", RunE: func(*cobra.Command, []string) error { return nil },
				PreRunE: func(cmd *cobra.Command, _ []string) error {
					return ExclusiveFlags(cmd, "inuse", "unused", "filter", "limit", "labels")
				},
			}
			cmd.SetContext(t.Context())
			cmd.Flags().Bool("inuse", false, "")
			cmd.Flags().Bool("unused", false, "")
			cmd.Flags().String("filter", "default", "")
			cmd.Flags().Int("limit", 10, "")
			cmd.Flags().StringSlice("labels", nil, "")
			require.NoError(t, cmd.ParseFlags(tt.args))
			checkErr := cmd.PreRunE(cmd, nil)
			if tt.wantErr != "" {
				require.EqualError(t, checkErr, tt.wantErr)
			} else {
				require.NoError(t, checkErr)
			}
		})
	}
}
