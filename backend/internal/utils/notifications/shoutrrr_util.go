package notifications

import (
	"fmt"
	"net/mail"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// BuildShoutrrrURL constructs a Shoutrrr-compatible URL from provider and configuration map
func BuildShoutrrrURL(provider string, config map[string]interface{}) (string, error) {
	switch provider {
	case "discord":
		return buildDiscordURL(config)
	case "telegram":
		return buildTelegramURL(config)
	case "slack":
		return buildSlackURL(config)
	case "gotify":
		return buildGotifyURL(config)
	case "ntfy":
		return buildNtfyURL(config)
	case "pushbullet":
		return buildPushbulletURL(config)
	case "pushover":
		return buildPushoverURL(config)
	case "email":
		return buildEmailURL(config)
	case "webhook":
		webhookURL, _ := config["webhookUrl"].(string)
		if webhookURL == "" {
			return "", fmt.Errorf("webhookUrl is required for webhook")
		}
		return webhookURL, nil
	default:
		urlStr, _ := config["url"].(string)
		if urlStr == "" {
			return "", fmt.Errorf("url is required for provider %q", provider)
		}
		return urlStr, nil
	}
}

func buildDiscordURL(config map[string]interface{}) (string, error) {
	webhookURL, _ := config["webhookUrl"].(string)
	if webhookURL == "" {
		return "", fmt.Errorf("webhookUrl is required for discord")
	}

	u, err := url.Parse(webhookURL)
	if err != nil {
		return "", fmt.Errorf("invalid discord webhook URL: %w", err)
	}

	// Format: https://discord.com/api/webhooks/ID/TOKEN
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")

	// Find "webhooks" in the path
	idx := -1
	for i, p := range parts {
		if p == "webhooks" {
			idx = i
			break
		}
	}

	if idx == -1 || len(parts) < idx+3 {
		return "", fmt.Errorf("invalid discord webhook URL format, expected https://discord.com/api/webhooks/ID/TOKEN")
	}

	id := parts[idx+1]
	token := parts[idx+2]

	return fmt.Sprintf("discord://%s@%s", token, id), nil
}

func buildTelegramURL(config map[string]interface{}) (string, error) {
	botToken, _ := config["botToken"].(string)
	chatID, _ := config["chatId"].(string)
	if botToken == "" || chatID == "" {
		return "", fmt.Errorf("botToken and chatId are required for telegram")
	}
	query := url.Values{}
	query.Set("chats", chatID)
	if silent, ok := config["sendSilently"].(bool); ok && silent {
		query.Set("notification", "no")
	}
	return fmt.Sprintf("telegram://%s@telegram?%s", botToken, query.Encode()), nil
}

func buildSlackURL(config map[string]interface{}) (string, error) {
	webhookURL, _ := config["webhookUrl"].(string)
	if webhookURL == "" {
		return "", fmt.Errorf("webhookUrl is required for slack")
	}

	u, err := url.Parse(webhookURL)
	if err != nil {
		return "", fmt.Errorf("invalid slack webhook URL: %w", err)
	}

	// Format: https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "services" {
		return "", fmt.Errorf("invalid slack webhook URL format, expected https://hooks.slack.com/services/T.../B.../XXX")
	}

	return fmt.Sprintf("slack://%s/%s/%s", parts[1], parts[2], parts[3]), nil
}

func buildGotifyURL(config map[string]interface{}) (string, error) {
	host, _ := config["url"].(string)
	token, _ := config["token"].(string)
	if host == "" || token == "" {
		return "", fmt.Errorf("url and token are required for gotify")
	}
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	query := url.Values{}
	if priority, ok := config["priority"].(float64); ok {
		query.Set("priority", fmt.Sprintf("%.0f", priority))
	} else if priority, ok := config["priority"].(int); ok {
		query.Set("priority", fmt.Sprintf("%d", priority))
	}
	return fmt.Sprintf("gotify://%s/%s?%s", host, token, query.Encode()), nil
}

func buildNtfyURL(config map[string]interface{}) (string, error) {
	host, _ := config["url"].(string)
	topic, _ := config["topic"].(string)
	if host == "" || topic == "" {
		return "", fmt.Errorf("url and topic are required for ntfy")
	}
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	query := url.Values{}
	if priority, ok := config["priority"].(float64); ok {
		query.Set("priority", fmt.Sprintf("%.0f", priority))
	} else if priority, ok := config["priority"].(int); ok {
		query.Set("priority", fmt.Sprintf("%d", priority))
	}
	userPass := ""
	username, _ := config["username"].(string)
	password, _ := config["password"].(string)
	if username != "" && password != "" {
		userPass = fmt.Sprintf("%s:%s@", username, password)
	}
	return fmt.Sprintf("ntfy://%s%s/%s?%s", userPass, host, topic, query.Encode()), nil
}

func buildPushbulletURL(config map[string]interface{}) (string, error) {
	token, _ := config["accessToken"].(string)
	if token == "" {
		return "", fmt.Errorf("accessToken is required for pushbullet")
	}
	channel, _ := config["channelTag"].(string)
	return fmt.Sprintf("pushbullet://%s/%s", token, channel), nil
}

func buildPushoverURL(config map[string]interface{}) (string, error) {
	token, _ := config["token"].(string)
	userKey, _ := config["userKey"].(string)
	if token == "" || userKey == "" {
		return "", fmt.Errorf("token and userKey are required for pushover")
	}
	query := url.Values{}
	if priority, ok := config["priority"].(float64); ok {
		query.Set("priority", fmt.Sprintf("%.0f", priority))
	} else if priority, ok := config["priority"].(int); ok {
		query.Set("priority", fmt.Sprintf("%d", priority))
	}
	if sound, _ := config["sound"].(string); sound != "" {
		query.Set("sound", sound)
	}
	return fmt.Sprintf("pushover://shoutrrr:%s@%s?%s", token, userKey, query.Encode()), nil
}

func buildEmailURL(config map[string]interface{}) (string, error) {
	host, _ := config["smtpHost"].(string)
	port, _ := config["smtpPort"].(float64)
	if host == "" || port == 0 {
		return "", fmt.Errorf("smtpHost and smtpPort are required for email")
	}
	user, _ := config["smtpUsername"].(string)
	pass, _ := config["smtpPassword"].(string)
	userPass := ""
	if user != "" && pass != "" {
		userPass = fmt.Sprintf("%s:%s@", user, pass)
	}
	query := url.Values{}
	if from, _ := config["fromAddress"].(string); from != "" {
		normalizedFrom, err := normalizeEmailAddress(from)
		if err != nil {
			return "", fmt.Errorf("invalid from email address %q: %w", from, err)
		}
		query.Set("from", normalizedFrom)
	}
	// shoutrrr accepts comma-separated emails for toaddresses (no spaces)
	if to, _ := config["toAddresses"].(string); to != "" {
		emails := strings.Split(to, ",")
		var validEmails []string
		for _, e := range emails {
			normalized, err := normalizeEmailAddress(e)
			if err != nil {
				trimmed := strings.TrimSpace(e)
				if trimmed == "" {
					continue
				}
				return "", fmt.Errorf("invalid to email address %q: %w", trimmed, err)
			}
			if normalized == "" {
				continue
			}
			validEmails = append(validEmails, normalized)
		}
		if len(validEmails) == 0 {
			return "", fmt.Errorf("no valid to email addresses provided")
		}
		query.Set("toaddresses", strings.Join(validEmails, ","))
	}

	tlsMode, _ := config["tlsMode"].(string)
	switch tlsMode {
	case "starttls":
		query.Set("usestarttls", "yes")
	case "ssl":
		query.Set("useimplicitssl", "yes")
		query.Set("usestarttls", "no")
	case "none":
		query.Set("usestarttls", "no")
	}

	if user != "" {
		query.Set("auth", "Plain")
	}

	if skipTLS, ok := config["skipTLSVerify"].(bool); ok && skipTLS {
		query.Set("skiptlsverify", "yes")
	}

	return fmt.Sprintf("smtp://%s%s:%.0f/?%s", userPass, host, port, query.Encode()), nil
}

var (
	idnaProfile = idna.New(
		idna.ValidateForRegistration(),
		idna.MapForLookup(),
	)
)

func normalizeEmailAddress(email string) (string, error) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", fmt.Errorf("email address is empty")
	}
	at := strings.LastIndex(trimmed, "@")
	if at <= 0 || at == len(trimmed)-1 {
		return "", fmt.Errorf("email address must contain local and domain parts")
	}
	local := trimmed[:at]
	domain := trimmed[at+1:]
	asciiDomain, err := idnaProfile.ToASCII(domain)
	if err != nil {
		return "", fmt.Errorf("invalid domain: %w", err)
	}
	normalized := fmt.Sprintf("%s@%s", local, asciiDomain)
	if _, err := mail.ParseAddress(normalized); err != nil {
		return "", fmt.Errorf("invalid address syntax: %w", err)
	}
	return normalized, nil
}
