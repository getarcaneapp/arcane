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
		return webhookURL, nil
	default:
		// For generic or unknown providers, assume the URL is already in the "url" field
		urlStr, _ := config["url"].(string)
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
	if len(match) == 3 {
		return fmt.Sprintf("discord://%s@%s", match[2], match[1]), nil
	}
	return webhookURL, nil
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
	if len(match) == 4 {
		return fmt.Sprintf("slack://%s/%s/%s", match[1], match[2], match[3]), nil
	}
	return webhookURL, nil
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
		query.Set("from", from)
	}
	if to, _ := config["toAddresses"].(string); to != "" {
		query.Set("to", to)
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

	return fmt.Sprintf("smtp://%s%s:%.0f/?%s", userPass, host, port, query.Encode()), nil
}
