package converter

import (
	"fmt"
	"strings"

	"github.com/getarcaneapp/arcane/backend/internal/models"
)

func ParseTokens(tokens []string, result *models.DockerRunCommand) error {
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]

		if strings.HasPrefix(token, "-") {
			advance, err := parseFlag(token, tokens, i, result)
			if err != nil {
				return err
			}
			i += advance
		} else {
			if result.Image == "" {
				if token == "" {
					return fmt.Errorf("image name cannot be empty")
				}
				result.Image = token
			} else {
				remainingTokens := tokens[i:]
				result.Command = strings.Join(remainingTokens, " ")
				break
			}
		}
	}
	return nil
}

func parseFlag(token string, tokens []string, index int, result *models.DockerRunCommand) (int, error) {
	switch token {
	case "-d", "--detach":
		result.Detached = true
		return 0, nil
	case "-i", "--interactive":
		result.Interactive = true
		return 0, nil
	case "-t", "--tty":
		result.TTY = true
		return 0, nil
	case "--rm":
		result.Remove = true
		return 0, nil
	case "--privileged":
		result.Privileged = true
		return 0, nil
	default:
		return parseFlagWithValue(token, tokens, index, result)
	}
}

func parseFlagWithValue(token string, tokens []string, index int, result *models.DockerRunCommand) (int, error) {
	switch token {
	case "--name":
		return parseStringFlag(token, tokens, index, &result.Name)
	case "-p", "--port", "--publish":
		return parseSliceFlag(token, tokens, index, &result.Ports)
	case "-v", "--volume":
		return parseSliceFlag(token, tokens, index, &result.Volumes)
	case "-e", "--env":
		return parseSliceFlag(token, tokens, index, &result.Environment)
	case "--network":
		return parseSliceFlag(token, tokens, index, &result.Networks)
	case "--restart":
		return parseStringFlag(token, tokens, index, &result.Restart)
	case "-w", "--workdir":
		return parseStringFlag(token, tokens, index, &result.Workdir)
	case "-u", "--user":
		return parseStringFlag(token, tokens, index, &result.User)
	case "--entrypoint":
		return parseStringFlag(token, tokens, index, &result.Entrypoint)
	case "--health-cmd":
		return parseStringFlag(token, tokens, index, &result.HealthCheck)
	case "-m", "--memory":
		return parseStringFlag(token, tokens, index, &result.MemoryLimit)
	case "--cpus":
		return parseStringFlag(token, tokens, index, &result.CPULimit)
	case "--label":
		return parseSliceFlag(token, tokens, index, &result.Labels)
	default:
		return parseUnknownFlag(token, tokens, index, result)
	}
}

func parseStringFlag(flagName string, tokens []string, index int, target *string) (int, error) {
	if index+1 >= len(tokens) {
		return 0, fmt.Errorf("missing value for %s flag", flagName)
	}

	value := tokens[index+1]
	if value == "" || strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("invalid value for %s flag", flagName)
	}

	*target = value
	return 1, nil
}

func parseSliceFlag(flagName string, tokens []string, index int, target *[]string) (int, error) {
	if index+1 >= len(tokens) {
		return 0, fmt.Errorf("missing value for %s flag", flagName)
	}

	value := tokens[index+1]
	if value == "" || strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("invalid value for %s flag", flagName)
	}

	*target = append(*target, value)
	return 1, nil
}

func parseUnknownFlag(token string, tokens []string, index int, result *models.DockerRunCommand) (int, error) {
	if !strings.HasPrefix(token, "--") && len(token) > 2 {
		parseCombinedFlags(token, result)
		return 0, nil
	}

	if index+1 < len(tokens) && !strings.HasPrefix(tokens[index+1], "-") && result.Image == "" {
		return 1, nil
	}

	return 0, nil
}

func parseCombinedFlags(token string, result *models.DockerRunCommand) {
	flags := strings.SplitSeq(token[1:], "")
	for flag := range flags {
		switch flag {
		case "d":
			result.Detached = true
		case "i":
			result.Interactive = true
		case "t":
			result.TTY = true
		}
	}
}

func ParseCommandTokens(command string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inQuotes := false
	var quoteChar rune

	for i, char := range command {
		switch {
		case (char == '"' || char == '\'') && !inQuotes:
			inQuotes = true
			quoteChar = char
		case char == quoteChar && inQuotes:
			inQuotes = false
			quoteChar = 0
		case char == ' ' && !inQuotes:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(char)
		}

		// Check for unclosed quotes at end
		if i == len(command)-1 && inQuotes {
			return nil, fmt.Errorf("unclosed quote in command: missing closing %c", quoteChar)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}
