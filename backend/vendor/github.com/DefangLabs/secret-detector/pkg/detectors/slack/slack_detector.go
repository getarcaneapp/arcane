package slack

import (
	"github.com/DefangLabs/secret-detector/pkg/detectors/helpers"
	"github.com/DefangLabs/secret-detector/pkg/secrets"
)

const (
	Name       = "slack"
	secretType = "Slack token"

	// tokensRegex represents a regex that matches a Slack token.
	//
	// Token types (with prefix):
	//   Bot Token (xoxb-)
	//   User Token (xoxp-)
	//   App-level token (xapp-)
	//   Configuration access token (xoxe.xoxb-, xoxe.xoxp-)
	//   Configuration refresh token (xoxe-)
	//   Legacy workspace access token (xoxa-2)
	//   Legacy workspace refresh token (xoxr-)
	//   Legacy tokens (xoxo-, xoxs-)
	// More information:
	//   https://api.slack.com/authentication/token-types
	//   https://api.slack.com/authentication/config-tokens
	//   https://api.slack.com/changelog/2016-05-19-authorship-changing-for-older-tokens
	tokensRegex = `(?:(?:xoxe\.)?xox[bp]|xox[aeosr]|xapp)-\d+-[-a-zA-Z0-9]+`

	// webhookRegex represents a regex that matches Slack webhook url
	// More information: https://docs.gitguardian.com/secrets-detection/detectors/specifics/slack_webhook_url
	webhookRegex = `https://hooks\.slack\.com/services/T[a-zA-Z0-9_]+/B[a-zA-Z0-9_]+/[a-zA-Z0-9_]+`
)

func init() {
	secrets.GetDetectorFactory().Register(Name, NewDetector)
}

type detector struct {
	secrets.Detector
}

func NewDetector(config ...string) secrets.Detector {
	return &detector{
		Detector: helpers.NewRegexDetectorBuilder(secretType, tokensRegex, webhookRegex).Build(),
	}
}
