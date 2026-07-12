package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/skarokin/discord-daily-log/internal/ask"
	"github.com/skarokin/discord-daily-log/internal/config"
	"github.com/skarokin/discord-daily-log/internal/discord"
	"github.com/skarokin/discord-daily-log/internal/store"
	"github.com/skarokin/discord-daily-log/internal/tasks"
)

type taskEnqueuer interface {
	Enqueue(context.Context, discord.TaskPayload, string, string) error
}

type Handler struct {
	cfg       config.Config
	publicKey ed25519.PublicKey
	enqueuer  taskEnqueuer
	ask       *ask.Service
	goals     store.GoalStore
	logger    *slog.Logger
}

func New(cfg config.Config, enqueuer taskEnqueuer, askService *ask.Service, goals store.GoalStore, logger *slog.Logger) *Handler {
	return &Handler{
		cfg: cfg, publicKey: ed25519.PublicKey(cfg.DiscordPublicKey), enqueuer: enqueuer,
		ask: askService, goals: goals, logger: logger,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("POST /interactions", h.interactions)
	mux.HandleFunc("POST /tasks/process", h.processTask)
	return securityHeaders(mux)
}

func (h *Handler) interactions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !verifyDiscordRequest(h.publicKey, r.Header, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var interaction discord.Interaction
	if err := json.Unmarshal(body, &interaction); err != nil {
		http.Error(w, "invalid interaction", http.StatusBadRequest)
		return
	}
	if interaction.Type == discord.InteractionPing {
		writeJSON(w, http.StatusOK, discord.InteractionResponse{Type: discord.InteractionPing})
		return
	}
	if interaction.Type != discord.InteractionApplicationCommand || !h.allowed(interaction) {
		writeJSON(w, http.StatusOK, discord.InteractionResponse{
			Type: discord.ResponseChannelMessage,
			Data: &discord.InteractionResponseData{Content: "This bot is private.", Flags: discord.MessageFlagEphemeral},
		})
		return
	}

	switch interaction.Data.Name {
	case "goal":
		h.goal(w, r, interaction)
	case "ask":
		h.askCommand(w, r, interaction)
	default:
		writeJSON(w, http.StatusOK, discord.InteractionResponse{
			Type: discord.ResponseChannelMessage,
			Data: &discord.InteractionResponseData{Content: "Unknown command.", Flags: discord.MessageFlagEphemeral},
		})
	}
}

func (h *Handler) goal(w http.ResponseWriter, r *http.Request, interaction discord.Interaction) {
	description := strings.TrimSpace(interaction.Data.StringOption("description"))
	if description != "" {
		if err := h.goals.Set(r.Context(), interaction.UserID(), description); err != nil {
			h.logger.Error("save goal", "error", err)
			writeJSON(w, http.StatusOK, discord.InteractionResponse{
				Type: discord.ResponseChannelMessage,
				Data: &discord.InteractionResponseData{Content: "I couldn't save that goal.", Flags: discord.MessageFlagEphemeral},
			})
			return
		}
	}
	goal, err := h.goals.Get(r.Context(), interaction.UserID())
	if err != nil {
		h.logger.Error("load goal", "error", err)
		goal = "Unavailable"
	}

	prefix := "Current goal:\n"
	if description != "" {
		prefix = "Goal updated:\n"
	}

	writeJSON(w, http.StatusOK, discord.InteractionResponse{
		Type: discord.ResponseChannelMessage,
		Data: &discord.InteractionResponseData{Content: prefix + goal, Flags: discord.MessageFlagEphemeral},
	})
}

func (h *Handler) askCommand(w http.ResponseWriter, r *http.Request, interaction discord.Interaction) {
	prompt := strings.TrimSpace(interaction.Data.StringOption("prompt"))
	if prompt == "" {
		prompt = "Summarize today's calories, macros, all available micronutrients, progress against my goals, and practical suggestions for the rest of the day."
	}

	payload := discord.TaskPayload{
		InteractionID: interaction.ID, InteractionToken: interaction.Token,
		ChannelID: interaction.ChannelID, UserID: interaction.UserID(), Prompt: prompt,
	}

	if h.cfg.DevMode {
		// dev mode: process immediately new goroutine
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()
			_ = h.ask.Process(ctx, payload)
		}()
	} else {
		// prod: since cloud run and http response = shutdown, we need to invoke a new instance to process the taskk
		audience := "https://" + r.Host
		if err := h.enqueuer.Enqueue(r.Context(), payload, audience+"/tasks/process", audience); err != nil {
			h.logger.Error("enqueue ask", "interaction_id", interaction.ID, "error", err)
			writeJSON(w, http.StatusOK, discord.InteractionResponse{
				Type: discord.ResponseChannelMessage,
				Data: &discord.InteractionResponseData{Content: "I couldn't queue that request.", Flags: discord.MessageFlagEphemeral},
			})
			return
		}
	}

	writeJSON(w, http.StatusOK, discord.InteractionResponse{Type: discord.ResponseDeferredMessage})
}

func (h *Handler) processTask(w http.ResponseWriter, r *http.Request) {
	if !h.cfg.DevMode {
		if r.Header.Get("X-CloudTasks-TaskName") == "" {
			http.Error(w, "missing task header", http.StatusUnauthorized)
			return
		}
		if err := tasks.ValidateOIDC(r.Context(), r.Header.Get("Authorization"), "https://"+r.Host, h.cfg.TaskServiceAccountEmail); err != nil {
			h.logger.Warn("reject task", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	var payload discord.TaskPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		http.Error(w, "invalid task", http.StatusBadRequest)
		return
	}
	if payload.UserID != h.cfg.AllowedUserID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.ask.Process(r.Context(), payload); err != nil {
		http.Error(w, "processing failed", http.StatusInternalServerError)
		return
	}

	// no content needed since we're gonna just edit the original response in the worker goroutine/instance
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) allowed(interaction discord.Interaction) bool {
	if interaction.UserID() != h.cfg.AllowedUserID || interaction.GuildID != h.cfg.AllowedGuildID {
		return false
	}
	if _, ok := h.cfg.AllowedChannelIDs[interaction.ChannelID]; ok {
		return true
	}
	if interaction.Channel != nil {
		_, ok := h.cfg.AllowedChannelIDs[interaction.Channel.ParentID]
		return ok
	}
	return false
}

func verifyDiscordRequest(publicKey ed25519.PublicKey, header http.Header, body []byte) bool {
	signature, err := hex.DecodeString(header.Get("X-Signature-Ed25519"))
	if err != nil {
		return false
	}

	timestamp := header.Get("X-Signature-Timestamp")
	if timestamp == "" {
		return false
	}

	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)).Abs() > 5*time.Minute {
		return false
	}

	message := append([]byte(timestamp), body...)
	return ed25519.Verify(publicKey, message, signature)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		_, _ = fmt.Fprint(w, "{}")
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
