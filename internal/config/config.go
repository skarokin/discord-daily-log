package config

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                    string
	GoogleCloudProject      string
	GoogleCloudLocation     string
	GeminiModel             string
	DiscordApplicationID    string
	DiscordPublicKey        []byte
	DiscordBotToken         string
	AllowedUserID           string
	AllowedGuildID          string
	AllowedChannelIDs       map[string]struct{}
	CloudTasksLocation      string
	CloudTasksQueue         string
	TaskServiceAccountEmail string
	USDAAPIKey              string
	GoalSeed                string
	DevMode                 bool
}

func Load() (Config, error) {
	cfg := Config{
		Port:                    env("PORT", "8080"),
		GoogleCloudProject:      os.Getenv("GOOGLE_CLOUD_PROJECT"),
		GoogleCloudLocation:     env("GOOGLE_CLOUD_LOCATION", "global"),
		GeminiModel:             env("GEMINI_MODEL", "gemini-3.5-flash"),
		DiscordApplicationID:    os.Getenv("DISCORD_APPLICATION_ID"),
		DiscordBotToken:         os.Getenv("DISCORD_BOT_TOKEN"),
		AllowedUserID:           os.Getenv("DISCORD_ALLOWED_USER_ID"),
		AllowedGuildID:          os.Getenv("DISCORD_ALLOWED_GUILD_ID"),
		AllowedChannelIDs:       csvSet(os.Getenv("DISCORD_ALLOWED_CHANNEL_IDS")),
		CloudTasksLocation:      env("CLOUD_TASKS_LOCATION", "us-central1"),
		CloudTasksQueue:         os.Getenv("CLOUD_TASKS_QUEUE"),
		TaskServiceAccountEmail: os.Getenv("TASK_SERVICE_ACCOUNT_EMAIL"),
		USDAAPIKey:              os.Getenv("USDA_API_KEY"),
		GoalSeed:                env("GOAL_SEED", "Track calories, protein, carbohydrates, fat, fiber, all available vitamins and minerals, and dietary variety/antioxidant-rich foods."),
		DevMode:                 envBool("DEV_MODE"),
	}

	publicKey := strings.TrimSpace(os.Getenv("DISCORD_PUBLIC_KEY"))
	if publicKey != "" {
		decoded, err := hex.DecodeString(publicKey)
		if err != nil {
			return Config{}, fmt.Errorf("decode DISCORD_PUBLIC_KEY: %w", err)
		}
		cfg.DiscordPublicKey = decoded
	}

	var missing []string
	required := map[string]string{
		"GOOGLE_CLOUD_PROJECT":        cfg.GoogleCloudProject,
		"DISCORD_APPLICATION_ID":      cfg.DiscordApplicationID,
		"DISCORD_BOT_TOKEN":           cfg.DiscordBotToken,
		"DISCORD_ALLOWED_USER_ID":     cfg.AllowedUserID,
		"DISCORD_ALLOWED_GUILD_ID":    cfg.AllowedGuildID,
		"DISCORD_ALLOWED_CHANNEL_IDS": strings.Join(keys(cfg.AllowedChannelIDs), ","),
		"USDA_API_KEY":                cfg.USDAAPIKey,
	}
	if len(cfg.DiscordPublicKey) == 0 {
		missing = append(missing, "DISCORD_PUBLIC_KEY")
	} else if len(cfg.DiscordPublicKey) != 32 {
		return Config{}, errors.New("DISCORD_PUBLIC_KEY must decode to 32 bytes")
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return Config{}, errors.New("missing required environment variables: " + strings.Join(missing, ", "))
	}
	if !cfg.DevMode {
		for name, value := range map[string]string{
			"CLOUD_TASKS_QUEUE":          cfg.CloudTasksQueue,
			"TASK_SERVICE_ACCOUNT_EMAIL": cfg.TaskServiceAccountEmail,
		} {
			if strings.TrimSpace(value) == "" {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) > 0 {
		return Config{}, errors.New("missing required environment variables: " + strings.Join(missing, ", "))
	}
	return cfg, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	value, _ := strconv.ParseBool(os.Getenv(name))
	return value
}

func csvSet(value string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result[item] = struct{}{}
		}
	}
	return result
}

func keys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	return result
}
