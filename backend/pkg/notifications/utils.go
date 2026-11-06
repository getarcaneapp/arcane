package notifications

import (
	"fmt"
	"strings"

	"github.com/nicholas-fedor/shoutrrr"
	shoutrrrTypes "github.com/nicholas-fedor/shoutrrr/pkg/types"
)

func SanitizeForEmail(text string) string {
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\t", " ")

	return strings.TrimSpace(text)
}

func sendShoutrrr(provider, destinationURL, message string, params *shoutrrrTypes.Params) error {
	sender, err := shoutrrr.CreateSenderWithOptions(shoutrrrTypes.SenderOptions{}, destinationURL)
	if err != nil {
		return fmt.Errorf("failed to create shoutrrr %s sender: %w", provider, err)
	}
	for _, err := range sender.Send(message, params) {
		if err != nil {
			return fmt.Errorf("failed to send %s message via shoutrrr: %w", provider, err)
		}
	}
	return nil
}
