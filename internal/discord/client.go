package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://discord.com/api/v10"

type Client struct {
	httpClient    *http.Client
	botToken      string
	applicationID string
}

func NewClient(botToken, applicationID string) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 2 {
					return http.ErrUseLastResponse
				}
				if !isDiscordCDN(req.URL) {
					return fmt.Errorf("refusing redirect outside Discord CDN")
				}
				return nil
			},
		},
		botToken:      botToken,
		applicationID: applicationID,
	}
}

func (c *Client) ListMessages(ctx context.Context, channelID string) ([]Message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/channels/"+url.PathEscape(channelID)+"/messages?limit=100", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bot "+c.botToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get Discord messages: %w", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("get Discord messages: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var messages []Message
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("decode Discord messages: %w", err)
	}

	// discord returns messages newest-first, so we need to reverse for the agent to see chronological order
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}

	return messages, nil
}

func (c *Client) DownloadImage(ctx context.Context, attachment Attachment, maxBytes int64) ([]byte, string, error) {
	if attachment.Size > maxBytes {
		return nil, "", fmt.Errorf("attachment %q exceeds size limit", attachment.Filename)
	}

	parsed, err := url.Parse(attachment.URL)
	if err != nil || !isDiscordCDN(parsed) {
		return nil, "", fmt.Errorf("attachment %q has an untrusted URL", attachment.Filename)
	}
	if !strings.HasPrefix(attachment.ContentType, "image/") {
		return nil, "", fmt.Errorf("attachment %q is not an image", attachment.Filename)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, attachment.URL, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("download attachment %q: %w", attachment.Filename, err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download attachment %q: status %d", attachment.Filename, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxBytes {
		return nil, "", fmt.Errorf("attachment %q exceeds size limit", attachment.Filename)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = attachment.ContentType
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("attachment %q returned non-image content", attachment.Filename)
	}

	return data, strings.Split(contentType, ";")[0], nil
}

// initial /ask command sends a "processing" response - we use this to edit that w the actual answer
func (c *Client) EditOriginalResponse(ctx context.Context, interactionToken, content string) error {
	content = formatDiscordMarkdown(content)
	chunks := splitMessageText(content, 1900)
	if len(chunks) == 0 {
		chunks = []string{"I couldn't generate a response."}
	}

	webhookEndpoint := apiBase + "/webhooks/" + url.PathEscape(c.applicationID) + "/" + url.PathEscape(interactionToken)
	if err := c.sendWebhookMessage(ctx, http.MethodPatch, webhookEndpoint+"/messages/@original", chunks[0]); err != nil {
		return fmt.Errorf("edit Discord response: %w", err)
	}
	for _, chunk := range chunks[1:] {
		if err := c.sendWebhookMessage(ctx, http.MethodPost, webhookEndpoint, chunk); err != nil {
			return fmt.Errorf("send Discord follow-up: %w", err)
		}
	}
	return nil
}

func (c *Client) sendWebhookMessage(ctx context.Context, method, endpoint, content string) error {
	body, err := json.Marshal(map[string]any{
		"content": content,
		"allowed_mentions": map[string]any{
			"parse": []string{},
		},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	return nil
}

func formatDiscordMarkdown(content string) string {
	lines := strings.Split(content, "\n")
	formatted := make([]string, 0, len(lines))
	for index := 0; index < len(lines); {
		if index+1 >= len(lines) || !isMarkdownTableRow(lines[index]) || !isMarkdownTableSeparator(lines[index+1]) {
			formatted = append(formatted, lines[index])
			index++
			continue
		}

		headers := markdownTableCells(lines[index])
		index += 2
		for index < len(lines) && isMarkdownTableRow(lines[index]) {
			cells := markdownTableCells(lines[index])
			if len(cells) == 0 {
				index++
				continue
			}
			var row strings.Builder
			fmt.Fprintf(&row, "**%s**", cells[0])
			for cellIndex := 1; cellIndex < len(cells) && cellIndex < len(headers); cellIndex++ {
				if cells[cellIndex] == "" || cells[cellIndex] == "—" {
					continue
				}
				fmt.Fprintf(&row, " · %s: %s", headers[cellIndex], cells[cellIndex])
			}
			formatted = append(formatted, row.String())
			index++
		}
	}
	return strings.Join(formatted, "\n")
}

func isMarkdownTableRow(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") && strings.Count(line, "|") >= 3
}

func isMarkdownTableSeparator(line string) bool {
	if !isMarkdownTableRow(line) {
		return false
	}
	for _, cell := range markdownTableCells(line) {
		cell = strings.Trim(cell, " :-")
		if cell != "" {
			return false
		}
	}
	return true
}

func markdownTableCells(line string) []string {
	line = strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(line, "|")
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

func splitMessageText(content string, limit int) []string {
	runes := []rune(content)
	chunks := make([]string, 0, (len(runes)+limit-1)/limit)
	for len(runes) > limit {
		split := limit
		for index := limit; index > limit/2; index-- {
			if runes[index-1] == '\n' {
				split = index
				break
			}
		}
		chunks = append(chunks, strings.TrimSpace(string(runes[:split])))
		runes = runes[split:]
	}
	if chunk := strings.TrimSpace(string(runes)); chunk != "" {
		chunks = append(chunks, chunk)
	}
	return chunks
}

func isDiscordCDN(value *url.URL) bool {
	if value == nil || value.Scheme != "https" {
		return false
	}
	host := strings.ToLower(value.Hostname())
	return host == "cdn.discordapp.com" || host == "media.discordapp.net"
}
