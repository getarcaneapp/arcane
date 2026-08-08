package s3

import (
	"testing"

	backuptypes "github.com/getarcaneapp/arcane/types/v2/backup"
	"github.com/stretchr/testify/require"
)

func TestFromCreateDestinationNormalizesConfiguration(t *testing.T) {
	configuration := FromCreateDestination(backuptypes.CreateS3Destination{
		Name:            " Offsite ",
		Endpoint:        " s3.example.com/ ",
		Bucket:          " backups ",
		Region:          " us-east-1 ",
		AccessKeyID:     " access-key ",
		SecretAccessKey: " secret-key ",
		Prefix:          " /production/ ",
		UseSSL:          true,
		ForcePathStyle:  true,
	})

	require.Equal(t, "Offsite", configuration.Name)
	require.Equal(t, "s3.example.com/", configuration.Endpoint)
	require.Equal(t, "backups", configuration.Bucket)
	require.Equal(t, "us-east-1", configuration.Region)
	require.Equal(t, "access-key", configuration.AccessKeyID)
	require.Equal(t, "secret-key", configuration.SecretAccessKey)
	require.Equal(t, "production", configuration.Prefix)
	require.Equal(t, "https://s3.example.com", configuration.EndpointURL())
}

func TestConfigurationValidateRequiresRegionOnlyForAWS(t *testing.T) {
	configuration := Configuration{Name: "Offsite", Bucket: "backups", AccessKeyID: "access-key", SecretAccessKey: "secret-key"}
	require.EqualError(t, configuration.Validate(true), "region is required for AWS S3")

	configuration.Endpoint = "s3.example.com"
	require.NoError(t, configuration.Validate(true))
}

func TestConfigurationRusticEnvironment(t *testing.T) {
	configuration := Configuration{
		Endpoint:        "http://s3.example.com/",
		Bucket:          "backups",
		AccessKeyID:     "access-key",
		SecretAccessKey: "secret-key",
		Prefix:          "/production/",
		UseSSL:          true,
		ForcePathStyle:  false,
	}

	require.Equal(t, []string{
		"RUSTIC_REPOSITORY=opendal:s3",
		"RUSTIC_REPO_OPT_BUCKET=backups",
		"RUSTIC_REPO_OPT_ROOT=/production/arcane-volume-backups/instance-1",
		"AWS_ACCESS_KEY_ID=access-key",
		"AWS_SECRET_ACCESS_KEY=secret-key",
		"AWS_EC2_METADATA_DISABLED=true",
		"RUSTIC_REPO_OPT_ENDPOINT=https://s3.example.com",
		"RUSTIC_REPO_OPT_REGION=auto",
		"AWS_REGION=auto",
		"RUSTIC_REPO_OPT_ENABLE_VIRTUAL_HOST_STYLE=true",
	}, configuration.RusticEnvironment("arcane-volume-backups", "instance-1"))
}
