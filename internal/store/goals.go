package store

import (
	"context"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GoalStore interface {
	Get(ctx context.Context, userID string) (string, error)
	Set(ctx context.Context, userID, goal string) error
	Close() error
}

type FirestoreGoals struct {
	client *firestore.Client
	seed   string
}

func NewFirestoreGoals(ctx context.Context, projectID, seed string) (*FirestoreGoals, error) {
	client, err := firestore.NewClient(ctx, projectID)
	if err != nil {
		return nil, err
	}

	return &FirestoreGoals{client: client, seed: seed}, nil
}

func (s *FirestoreGoals) Get(ctx context.Context, userID string) (string, error) {
	snapshot, err := s.client.Collection("users").Doc(userID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return s.seed, nil
		}
		return "", err
	}

	value, err := snapshot.DataAt("goal")
	if err != nil {
		return s.seed, nil
	}

	goal, ok := value.(string)
	if !ok || goal == "" {
		return s.seed, nil
	}

	return goal, nil
}

func (s *FirestoreGoals) Set(ctx context.Context, userID, goal string) error {
	_, err := s.client.Collection("users").Doc(userID).Set(ctx, map[string]any{
		"goal":       goal,
		"updated_at": time.Now().UTC(),
	}, firestore.MergeAll)
	return err
}

func (s *FirestoreGoals) Close() error {
	return s.client.Close()
}

type MemoryGoals struct {
	mu     sync.RWMutex
	values map[string]string
	seed   string
}

func NewMemoryGoals(seed string) *MemoryGoals {
	return &MemoryGoals{values: make(map[string]string), seed: seed}
}

func (s *MemoryGoals) Get(_ context.Context, userID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if value := s.values[userID]; value != "" {
		return value, nil
	}

	return s.seed, nil
}

func (s *MemoryGoals) Set(_ context.Context, userID, goal string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[userID] = goal

	return nil
}

func (s *MemoryGoals) Close() error { return nil }
