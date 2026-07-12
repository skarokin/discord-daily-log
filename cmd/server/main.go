package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skarokin/discord-daily-log/internal/ai"
	"github.com/skarokin/discord-daily-log/internal/ask"
	"github.com/skarokin/discord-daily-log/internal/config"
	"github.com/skarokin/discord-daily-log/internal/discord"
	"github.com/skarokin/discord-daily-log/internal/httpapi"
	"github.com/skarokin/discord-daily-log/internal/store"
	"github.com/skarokin/discord-daily-log/internal/tasks"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()

	agentService, err := ai.New(ctx, cfg.GoogleCloudProject, cfg.GoogleCloudLocation, cfg.GeminiModel, cfg.USDAAPIKey)
	if err != nil {
		logger.Error("initialize nutrition agent", "error", err)
		os.Exit(1)
	}
	defer agentService.Close()

	var goals store.GoalStore
	if cfg.DevMode {
		goals = store.NewMemoryGoals(cfg.GoalSeed)
	} else {
		goals, err = store.NewFirestoreGoals(ctx, cfg.GoogleCloudProject, cfg.GoalSeed)
		if err != nil {
			logger.Error("initialize Firestore", "error", err)
			os.Exit(1)
		}
	}
	defer goals.Close()

	discordClient := discord.NewClient(cfg.DiscordBotToken, cfg.DiscordApplicationID)
	askService := ask.New(discordClient, agentService, goals, logger)

	// cloud tasks necessary for discord 3-sec ack deadline in prod since cloud run. if dev then no need since not serverless
	var enqueuer *tasks.Enqueuer
	if !cfg.DevMode {
		enqueuer, err = tasks.NewEnqueuer(
			ctx, cfg.GoogleCloudProject, cfg.CloudTasksLocation, cfg.CloudTasksQueue,
			cfg.TaskServiceAccountEmail,
		)
		if err != nil {
			logger.Error("initialize Cloud Tasks", "error", err)
			os.Exit(1)
		}
		defer enqueuer.Close()
	}

	handler := httpapi.New(cfg, enqueuer, askService, goals, logger)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("server listening", "port", cfg.Port, "dev_mode", cfg.DevMode)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server stopped", "error", err)
			os.Exit(1)
		}
	}()

	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	<-stop.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown", "error", err)
	}
}
