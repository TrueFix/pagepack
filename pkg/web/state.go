package web

import (
	"context"
	"encoding/json"
	"time"

	"github.com/valkey-io/valkey-go"
)

// storedJob keeps the private object key out of public JSON responses.
type storedJob struct {
	Job       Job    `json:"job"`
	ObjectKey string `json:"object_key,omitempty"`
}

type StateStore interface {
	Save(context.Context, Job) error
	Get(context.Context, string) (Job, error)
	Ping(context.Context) error
}

type ValkeyState struct {
	client valkey.Client
	ttl    time.Duration
}

func (s *ValkeyState) Save(ctx context.Context, job Job) error {
	data, err := json.Marshal(storedJob{Job: job, ObjectKey: job.ObjectKey})
	if err != nil {
		return err
	}
	return s.client.Do(ctx, s.client.B().Set().Key("pagepack:jobs:"+job.ID).Value(string(data)).Ex(s.ttl).Build()).Error()
}

func (s *ValkeyState) Get(ctx context.Context, id string) (Job, error) {
	data, err := s.client.Do(ctx, s.client.B().Get().Key("pagepack:jobs:"+id).Build()).ToString()
	if err != nil {
		return Job{}, err
	}
	var stored storedJob
	err = json.Unmarshal([]byte(data), &stored)
	stored.Job.ObjectKey = stored.ObjectKey
	return stored.Job, err
}

func (s *ValkeyState) Ping(ctx context.Context) error {
	return s.client.Do(ctx, s.client.B().Ping().Build()).Error()
}
