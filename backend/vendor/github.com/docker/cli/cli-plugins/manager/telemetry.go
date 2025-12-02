package manager

import (
	"fmt"
	"os"
	"strings"

	"github.com/docker/cli/cli-plugins/metadata"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
)

const (
	// resourceAttributesEnvVar is the name of the envvar that includes additional
	// resource attributes for OTEL as defined in the [OpenTelemetry specification].
	//
	// [OpenTelemetry specification]: https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/#general-sdk-configuration
	resourceAttributesEnvVar = "OTEL_RESOURCE_ATTRIBUTES"

	// dockerCLIAttributePrefix is the prefix for any docker cli OTEL attributes.
	//
	// It is a copy of the const defined in [command.dockerCLIAttributePrefix].
	dockerCLIAttributePrefix = "docker.cli."
	cobraCommandPath         = attribute.Key("cobra.command_path")
)

func getPluginResourceAttributes(cmd *cobra.Command, plugin Plugin) attribute.Set {
	commandPath := cmd.Annotations[metadata.CommandAnnotationPluginCommandPath]
	if commandPath == "" {
		commandPath = fmt.Sprintf("%s %s", cmd.CommandPath(), plugin.Name)
	}

	attrSet := attribute.NewSet(
		cobraCommandPath.String(commandPath),
	)

	kvs := make([]attribute.KeyValue, 0, attrSet.Len())
	for iter := attrSet.Iter(); iter.Next(); {
		attr := iter.Attribute()
		kvs = append(kvs, attribute.KeyValue{
			Key:   dockerCLIAttributePrefix + attr.Key,
			Value: attr.Value,
		})
	}
	return attribute.NewSet(kvs...)
}

func appendPluginResourceAttributesEnvvar(env []string, cmd *cobra.Command, plugin Plugin) []string {
	if attrs := getPluginResourceAttributes(cmd, plugin); attrs.Len() > 0 {
		// Construct baggage members for each of the attributes.
		// Ignore any failures as these aren't significant and
		// represent an internal issue.
		members := make([]baggage.Member, 0, attrs.Len())
		for iter := attrs.Iter(); iter.Next(); {
			attr := iter.Attribute()
			m, err := baggage.NewMemberRaw(string(attr.Key), attr.Value.AsString())
			if err != nil {
				otel.Handle(err)
				continue
			}
			members = append(members, m)
		}

		// Combine plugin added resource attributes with ones found in the environment
		// variable. Our own attributes should be namespaced so there shouldn't be a
		// conflict. We do not parse the environment variable because we do not want
		// to handle errors in user configuration.
		attrsSlice := make([]string, 0, 2)
		if v := strings.TrimSpace(os.Getenv(resourceAttributesEnvVar)); v != "" {
			attrsSlice = append(attrsSlice, v)
		}
		if b, err := baggage.New(members...); err != nil {
			otel.Handle(err)
		} else if b.Len() > 0 {
			attrsSlice = append(attrsSlice, b.String())
		}

		if len(attrsSlice) > 0 {
			env = append(env, resourceAttributesEnvVar+"="+strings.Join(attrsSlice, ","))
		}
	}
	return env
}
