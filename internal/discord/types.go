package discord

import "encoding/json"

const (
	InteractionPing               = 1
	InteractionApplicationCommand = 2

	ResponseChannelMessage  = 4
	ResponseDeferredMessage = 5

	MessageFlagEphemeral = 1 << 6
)

type Interaction struct {
	ID        string          `json:"id"`
	Type      int             `json:"type"`
	Token     string          `json:"token"`
	GuildID   string          `json:"guild_id"`
	ChannelID string          `json:"channel_id"`
	Channel   *Channel        `json:"channel"`
	Member    *Member         `json:"member"`
	User      *User           `json:"user"`
	Data      InteractionData `json:"data"`
}

func (i Interaction) UserID() string {
	if i.Member != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

type InteractionData struct {
	Name    string              `json:"name"`
	Options []InteractionOption `json:"options"`
}

func (d InteractionData) StringOption(name string) string {
	for _, option := range d.Options {
		if option.Name == name {
			var value string
			_ = json.Unmarshal(option.Value, &value)
			return value
		}
	}
	return ""
}

type InteractionOption struct {
	Name  string          `json:"name"`
	Type  int             `json:"type"`
	Value json.RawMessage `json:"value"`
}

type Channel struct {
	ID       string `json:"id"`
	ParentID string `json:"parent_id"`
}

type Member struct {
	User User `json:"user"`
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

type InteractionResponse struct {
	Type int                      `json:"type"`
	Data *InteractionResponseData `json:"data,omitempty"`
}

type InteractionResponseData struct {
	Content string `json:"content,omitempty"`
	Flags   int    `json:"flags,omitempty"`
}

type Message struct {
	ID          string       `json:"id"`
	Content     string       `json:"content"`
	Timestamp   string       `json:"timestamp"`
	EditedAt    *string      `json:"edited_timestamp"`
	Author      User         `json:"author"`
	Attachments []Attachment `json:"attachments"`
}

type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type TaskPayload struct {
	InteractionID    string `json:"interaction_id"`
	InteractionToken string `json:"interaction_token"`
	ChannelID        string `json:"channel_id"`
	UserID           string `json:"user_id"`
	Prompt           string `json:"prompt"`
}
