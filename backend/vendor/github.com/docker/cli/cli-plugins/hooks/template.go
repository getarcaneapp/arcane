package hooks

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
)

type HookType int

const (
	NextSteps = iota
)

// HookMessage represents a plugin hook response. Plugins
// declaring support for CLI hooks need to print a json
// representation of this type when their hook subcommand
// is invoked.
type HookMessage struct {
	Type     HookType
	Template string
}

// TemplateReplaceSubcommandName returns a hook template string
// that will be replaced by the CLI subcommand being executed
//
// Example:
//
// "you ran the subcommand: " + TemplateReplaceSubcommandName()
//
// when being executed after the command:
// `docker run --name "my-container" alpine`
// will result in the message:
// `you ran the subcommand: run`
func TemplateReplaceSubcommandName() string {
	return hookTemplateCommandName
}

// TemplateReplaceFlagValue returns a hook template string
// that will be replaced by the flags value.
//
// Example:
//
// "you ran a container named: " + TemplateReplaceFlagValue("name")
//
// when being executed after the command:
// `docker run --name "my-container" alpine`
// will result in the message:
// `you ran a container named: my-container`
func TemplateReplaceFlagValue(flag string) string {
	return fmt.Sprintf(hookTemplateFlagValue, flag)
}

// TemplateReplaceArg takes an index i and returns a hook
// template string that the CLI will replace the template with
// the ith argument, after processing the passed flags.
//
// Example:
//
// "run this image with `docker run " + TemplateReplaceArg(0) + "`"
//
// when being executed after the command:
// `docker pull alpine`
// will result in the message:
// "Run this image with `docker run alpine`"
func TemplateReplaceArg(i int) string {
	return fmt.Sprintf(hookTemplateArg, strconv.Itoa(i))
}

func ParseTemplate(hookTemplate string, cmd *cobra.Command) ([]string, error) {
	tmpl := template.New("").Funcs(commandFunctions)
	tmpl, err := tmpl.Parse(hookTemplate)
	if err != nil {
		return nil, err
	}
	b := bytes.Buffer{}
	err = tmpl.Execute(&b, cmd)
	if err != nil {
		return nil, err
	}
	return strings.Split(b.String(), "\n"), nil
}

var ErrHookTemplateParse = errors.New("failed to parse hook template")

const (
	hookTemplateCommandName = "{{.Name}}"
	hookTemplateFlagValue   = `{{flag . "%s"}}`
	hookTemplateArg         = "{{arg . %s}}"
)

var commandFunctions = template.FuncMap{
	"flag": getFlagValue,
	"arg":  getArgValue,
}

func getFlagValue(cmd *cobra.Command, flag string) (string, error) {
	cmdFlag := cmd.Flag(flag)
	if cmdFlag == nil {
		return "", ErrHookTemplateParse
	}
	return cmdFlag.Value.String(), nil
}

func getArgValue(cmd *cobra.Command, i int) (string, error) {
	flags := cmd.Flags()
	if flags == nil {
		return "", ErrHookTemplateParse
	}
	return flags.Arg(i), nil
}
