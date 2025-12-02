package docker

import (
	"github.com/Masterminds/semver/v3"
)

type mobyBuildkitVersion struct {
	MobyVersionConstraint string
	BuildkitVersion       string
}

// https://gist.github.com/crazy-max/780cb6878c37cb79ec3f7699706cf83f
// constraint syntax: https://github.com/Masterminds/semver#checking-version-constraints
var mobyBuildkitVersions = []mobyBuildkitVersion{
	{
		MobyVersionConstraint: ">= 18.06.0-0, < 18.06.1-0",
		BuildkitVersion:       "v0.0.0+9acf51e",
	},
	{
		MobyVersionConstraint: ">= 18.06.1-0, < 18.09.0-0",
		BuildkitVersion:       "v0.0.0+98f1604",
	},
	{
		MobyVersionConstraint: ">= 18.09.0-0, < 18.09.1-0",
		BuildkitVersion:       "v0.0.0+c7bb575",
	},
	{
		MobyVersionConstraint: "~18.09.1-0",
		BuildkitVersion:       "v0.3.3",
	},
	{
		MobyVersionConstraint: "> 18.09.1-0, < 18.09.6-0",
		BuildkitVersion:       "v0.3.3+d9f7592",
	},
	{
		MobyVersionConstraint: ">= 18.09.6-0, < 18.09.7-0",
		BuildkitVersion:       "v0.4.0+ed4da8b",
	},
	{
		MobyVersionConstraint: ">= 18.09.7-0, < 19.03.0-0",
		BuildkitVersion:       "v0.4.0+05766c5",
	},
	{
		MobyVersionConstraint: "<= 19.03.0-beta2",
		BuildkitVersion:       "v0.4.0+b302896",
	},
	{
		MobyVersionConstraint: "<= 19.03.0-beta3",
		BuildkitVersion:       "v0.4.0+8818c67",
	},
	{
		MobyVersionConstraint: "<= 19.03.0-beta5",
		BuildkitVersion:       "v0.5.1+f238f1e",
	},
	{
		MobyVersionConstraint: "< 19.03.2-0",
		BuildkitVersion:       "v0.5.1+1f89ec1",
	},
	{
		MobyVersionConstraint: "<= 19.03.2-beta1",
		BuildkitVersion:       "v0.6.1",
	},
	{
		MobyVersionConstraint: ">= 19.03.2-0, < 19.03.3-0",
		BuildkitVersion:       "v0.6.1+588c73e",
	},
	{
		MobyVersionConstraint: ">= 19.03.3-0, < 19.03.5-beta2",
		BuildkitVersion:       "v0.6.2",
	},
	{
		MobyVersionConstraint: "<= 19.03.5-rc1",
		BuildkitVersion:       "v0.6.2+ff93519",
	},
	{
		MobyVersionConstraint: "<= 19.03.5",
		BuildkitVersion:       "v0.6.3+928f3b4",
	},
	{
		MobyVersionConstraint: "<= 19.03.6-rc1",
		BuildkitVersion:       "v0.6.3+926935b",
	},
	{
		MobyVersionConstraint: ">= 19.03.6-rc2, < 19.03.7-0",
		BuildkitVersion:       "v0.6.3+57e8ad5",
	},
	{
		MobyVersionConstraint: ">= 19.03.7-0, < 19.03.9-0",
		BuildkitVersion:       "v0.6.4",
	},
	{
		MobyVersionConstraint: ">= 19.03.9-0, < 19.03.13-0",
		BuildkitVersion:       "v0.6.4+a7d7b7f",
	},
	{
		MobyVersionConstraint: "<= 19.03.13-beta2",
		BuildkitVersion:       "v0.6.4+da1f4bf",
	},
	{
		MobyVersionConstraint: "<= 19.03.14",
		BuildkitVersion:       "v0.6.4+df89d4d",
	},
	{
		MobyVersionConstraint: "< 20.10.0",
		BuildkitVersion:       "v0.6.4+396bfe2",
	},
	{
		MobyVersionConstraint: "20.10.0-0 - 20.10.2-0",
		BuildkitVersion:       "v0.8.1",
	},
	{
		MobyVersionConstraint: ">= 20.10.3-0, < 20.10.4-0",
		BuildkitVersion:       "v0.8.1+68bb095",
	},
	{
		MobyVersionConstraint: "20.10.4-0 - 20.10.6",
		BuildkitVersion:       "v0.8.2",
	},
	{
		MobyVersionConstraint: "20.10.7-0 - 20.10.10-0",
		BuildkitVersion:       "v0.8.2+244e8cde",
	},
	{
		MobyVersionConstraint: "20.10.11-0 - 20.10.18-0",
		BuildkitVersion:       "v0.8.2+bc07b2b8",
	},
	{
		MobyVersionConstraint: ">= 20.10.19-0, < 20.10.20-0",
		BuildkitVersion:       "v0.8.2+3a1eeca5",
	},
	{
		MobyVersionConstraint: ">= 20.10.20-0, < 20.10.21-0",
		BuildkitVersion:       "v0.8.2+c0149372",
	},
	{
		MobyVersionConstraint: ">= 20.10.21-0, <= 20.10.23",
		BuildkitVersion:       "v0.8.2+eeb7b65",
	},
	{
		MobyVersionConstraint: "~20.10-0",
		BuildkitVersion:       "v0.8+unknown",
	},
	{
		MobyVersionConstraint: "~22.06-0",
		BuildkitVersion:       "v0.10.3",
	},
	{
		MobyVersionConstraint: ">= 23.0.0-0, < 23.0.1-0",
		BuildkitVersion:       "v0.10.6",
	},
	{
		MobyVersionConstraint: "23.0.1",
		BuildkitVersion:       "v0.10.6+4f0ee09",
	},
	{
		MobyVersionConstraint: ">= 23.0.2-0, < 23.0.4-0",
		BuildkitVersion:       "v0.10.6+70f2ad5",
	},
	{
		MobyVersionConstraint: ">= 23.0.4-0, < 23.0.7-0",
		BuildkitVersion:       "v0.10.6+d52b2d5",
	},
	{
		MobyVersionConstraint: "~23-0",
		BuildkitVersion:       "v0.10+unknown",
	},
}

func resolveBuildKitVersion(ver string) (string, error) {
	mobyVersion, err := semver.NewVersion(ver)
	if err != nil {
		return "", err
	}
	for _, m := range mobyBuildkitVersions {
		c, err := semver.NewConstraint(m.MobyVersionConstraint)
		if err != nil {
			return "", err
		}
		if !c.Check(mobyVersion) {
			continue
		}
		return m.BuildkitVersion, nil
	}
	return "", nil
}
