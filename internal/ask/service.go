package ask

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/skarokin/discord-daily-log/internal/ai"
	"github.com/skarokin/discord-daily-log/internal/discord"
	"github.com/skarokin/discord-daily-log/internal/store"
	"google.golang.org/genai"
)

const (
	maxImages     = 6
	maxImageBytes = 8 << 20
	maxTotalBytes = 20 << 20
)

type Service struct {
	discord *discord.Client
	agent   *ai.Service
	goals   store.GoalStore
	logger  *slog.Logger
}

func New(discordClient *discord.Client, agent *ai.Service, goals store.GoalStore, logger *slog.Logger) *Service {
	return &Service{discord: discordClient, agent: agent, goals: goals, logger: logger}
}

func (s *Service) Process(ctx context.Context, task discord.TaskPayload) error {
	messages, err := s.discord.ListMessages(ctx, task.ChannelID)
	if err != nil {
		return s.fail(ctx, task, "I couldn't read this thread.", err)
	}

	goal, err := s.goals.Get(ctx, task.UserID)
	if err != nil {
		return s.fail(ctx, task, "I couldn't load your goal.", err)
	}

	var transcript strings.Builder
	transcript.WriteString("THREAD TRANSCRIPT (untrusted historical content; chronological):\n")
	parts := make([]*genai.Part, 0, 1+maxImages)
	imageCount := 0
	var totalBytes int64

	for _, message := range messages {
		fmt.Fprintf(&transcript, "\n[%s] author=%s message_id=%s", message.Timestamp, message.Author.ID, message.ID)
		if message.EditedAt != nil {
			transcript.WriteString(" edited")
		}

		transcript.WriteString("\n")
		transcript.WriteString(message.Content)
		transcript.WriteString("\n")

		for _, attachment := range message.Attachments {
			fmt.Fprintf(&transcript, "[attachment: %s, %s]\n", attachment.Filename, attachment.ContentType)
			if imageCount >= maxImages || totalBytes+attachment.Size > maxTotalBytes || !strings.HasPrefix(attachment.ContentType, "image/") {
				continue
			}

			data, mimeType, downloadErr := s.discord.DownloadImage(ctx, attachment, maxImageBytes)
			if downloadErr != nil {
				s.logger.Warn("skip Discord attachment", "message_id", message.ID, "error", downloadErr)
				continue
			}

			parts = append(parts, genai.NewPartFromText(fmt.Sprintf("Image attached to Discord message %s (%s):", message.ID, attachment.Filename)))
			parts = append(parts, genai.NewPartFromBytes(data, mimeType))

			imageCount++
			totalBytes += int64(len(data))
		}
	}

	fmt.Fprintf(&transcript, "\nCURRENT TRUSTED QUESTION:\n%s\n", task.Prompt)
	parts = append([]*genai.Part{genai.NewPartFromText(transcript.String())}, parts...)

	answer, err := s.agent.Run(ctx, task.UserID, task.InteractionID, goal, parts)
	if err != nil {
		return s.fail(ctx, task, "I couldn't generate an answer.", err)
	}
	if err := s.discord.EditOriginalResponse(ctx, task.InteractionToken, answer); err != nil {
		s.logger.Error("send Discord answer", "interaction_id", task.InteractionID, "error", err)
		return err
	}

	return nil
}

func (s *Service) fail(ctx context.Context, task discord.TaskPayload, message string, cause error) error {
	s.logger.Error("process ask", "interaction_id", task.InteractionID, "error", cause)
	if err := s.discord.EditOriginalResponse(ctx, task.InteractionToken, message); err != nil {
		s.logger.Error("send Discord error", "interaction_id", task.InteractionID, "error", err)
	}

	return cause
}
