package notifications

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
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
	// Extract token and id from discord webhook URL
	// Format: https://discord.com/api/webhooks/ID/TOKEN
	re := regexp.MustCompile(`webhooks/(\d+)/(.+)`)
	match := re.FindStringSubmatch(webhookURL)
	if len(match) != 3 {
		return "", fmt.Errorf("invalid discord webhook URL format, expected https://discord.com/api/webhooks/ID/TOKEN")
	}
	return fmt.Sprintf("discord://%s@%s", match[2], match[1]), nil
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
	// Format: https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX
	re := regexp.MustCompile(`services/(.+)/(.+)/(.+)`)
	match := re.FindStringSubmatch(webhookURL)
	if len(match) != 4 {
		return "", fmt.Errorf("invalid slack webhook URL format, expected https://hooks.slack.com/services/T.../B.../XXX")
	}
	return fmt.Sprintf("slack://%s/%s/%s", match[1], match[2], match[3]), nil
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
		from = strings.TrimSpace(from)
		if !isValidEmail(from) {
			return "", fmt.Errorf("invalid from email address format: %s", from)
		}
		query.Set("from", from)
	}
	// shoutrrr accepts comma-separated emails for toaddresses (no spaces)
	if to, _ := config["toAddresses"].(string); to != "" {
		emails := strings.Split(to, ",")
		var validEmails []string
		for _, e := range emails {
			trimmed := strings.TrimSpace(e)
			if trimmed == "" {
				continue
			}
			if !isValidEmail(trimmed) {
				return "", fmt.Errorf("invalid to email address format: %s", trimmed)
			}
			validEmails = append(validEmails, trimmed)
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

var emailRegex = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}$`)

func isValidEmail(email string) bool {
	return emailRegex.MatchString(strings.ToLower(email))
}
