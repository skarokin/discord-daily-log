package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cloudtasks "cloud.google.com/go/cloudtasks/apiv2"
	"cloud.google.com/go/cloudtasks/apiv2/cloudtaskspb"
	"github.com/skarokin/discord-daily-log/internal/discord"
	"google.golang.org/api/idtoken"
)

type Enqueuer struct {
	client              *cloudtasks.Client
	parent              string
	serviceAccountEmail string
}

func NewEnqueuer(ctx context.Context, project, location, queue, serviceAccountEmail string) (*Enqueuer, error) {
	client, err := cloudtasks.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return &Enqueuer{
		client:              client,
		parent:              fmt.Sprintf("projects/%s/locations/%s/queues/%s", project, location, queue),
		serviceAccountEmail: serviceAccountEmail,
	}, nil
}

func (e *Enqueuer) Enqueue(ctx context.Context, payload discord.TaskPayload, targetURL, audience string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	_, err = e.client.CreateTask(ctx, &cloudtaskspb.CreateTaskRequest{
		Parent: e.parent,
		Task: &cloudtaskspb.Task{
			MessageType: &cloudtaskspb.Task_HttpRequest{
				HttpRequest: &cloudtaskspb.HttpRequest{
					HttpMethod: cloudtaskspb.HttpMethod_POST,
					Url:        targetURL,
					Headers:    map[string]string{"Content-Type": "application/json"},
					Body:       body,
					AuthorizationHeader: &cloudtaskspb.HttpRequest_OidcToken{
						OidcToken: &cloudtaskspb.OidcToken{
							ServiceAccountEmail: e.serviceAccountEmail,
							Audience:            audience,
						},
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create Cloud Task: %w", err)
	}

	return nil
}

func (e *Enqueuer) Close() error {
	return e.client.Close()
}

func ValidateOIDC(ctx context.Context, authorization, audience, expectedEmail string) error {
	token := strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
	if token == "" || token == authorization {
		return fmt.Errorf("missing bearer token")
	}

	payload, err := idtoken.Validate(ctx, token, audience)
	if err != nil {
		return fmt.Errorf("validate task token: %w", err)
	}

	email, _ := payload.Claims["email"].(string)
	emailVerified, _ := payload.Claims["email_verified"].(bool)
	if email != expectedEmail || !emailVerified {
		return fmt.Errorf("unexpected task identity")
	}

	return nil
}
