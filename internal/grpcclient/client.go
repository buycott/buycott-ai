// Package grpcclient implements server.Server over a gRPC connection so all
// cmd/ commands work unchanged when --server host:port is provided.
package grpcclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"buycott/internal/grpcapi"
	"buycott/internal/model"
	"buycott/internal/server"
	"buycott/internal/state"
)

// Client wraps a gRPC connection and implements server.Server.
type Client struct {
	conn *grpc.ClientConn
	rpc  grpcapi.BuycottServiceClient
}

var _ server.Server = (*Client)(nil)

// Dial connects to a remote buycott gRPC server.
func Dial(target string) (*Client, error) {
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", target, err)
	}
	return &Client{conn: conn, rpc: grpcapi.NewBuycottServiceClient(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// ---------- pipeline control ----------

func (c *Client) Start(_ context.Context, _ string) error {
	return fmt.Errorf("start: not supported over remote connection")
}

func (c *Client) Stop() error {
	return fmt.Errorf("stop: not supported over remote connection")
}

func (c *Client) Pause() error {
	_, err := c.rpc.Pause(context.Background(), &grpcapi.Empty{})
	return err
}

func (c *Client) Resume() error {
	_, err := c.rpc.Resume(context.Background(), &grpcapi.Empty{})
	return err
}

func (c *Client) GetStatus() (server.Status, error) {
	resp, err := c.rpc.GetStatus(context.Background(), &grpcapi.Empty{})
	if err != nil {
		return server.Status{}, err
	}
	st := server.Status{
		Running:     resp.Running,
		Paused:      resp.Paused,
		QueueLength: int(resp.QueueLength),
		Completed:   int(resp.Completed),
		Escalated:   int(resp.Escalated),
	}
	if resp.ActiveTask != nil {
		t := fromProtoTask(resp.ActiveTask)
		st.ActiveTask = &t
	}
	return st, nil
}

// ---------- tasks ----------

func (c *Client) GetTask(id string) (*model.Task, error) {
	resp, err := c.rpc.GetTask(context.Background(), &grpcapi.GetTaskRequest{Id: id})
	if err != nil {
		return nil, err
	}
	t := fromProtoTask(resp.Task)
	return &t, nil
}

func (c *Client) ListTasks(filter model.TaskFilter) ([]*model.Task, error) {
	resp, err := c.rpc.ListTasks(context.Background(), &grpcapi.ListTasksRequest{
		StatusFilter: string(filter.Status),
	})
	if err != nil {
		return nil, err
	}
	var tasks []*model.Task
	for _, pt := range resp.Tasks {
		t := fromProtoTask(pt)
		tasks = append(tasks, &t)
	}
	return tasks, nil
}

// ---------- events ----------

func (c *Client) ListEvents(limit int) ([]*model.Event, error) {
	resp, err := c.rpc.ListEvents(context.Background(), &grpcapi.ListEventsRequest{
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	var events []*model.Event
	for _, pe := range resp.Events {
		e := fromProtoEvent(pe)
		events = append(events, &e)
	}
	return events, nil
}

func (c *Client) StreamEvents(ctx context.Context) (<-chan model.Event, error) {
	stream, err := c.rpc.StreamEvents(ctx, &grpcapi.Empty{})
	if err != nil {
		return nil, err
	}
	ch := make(chan model.Event, 64)
	go func() {
		defer close(ch)
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			e := fromProtoEvent(msg)
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// ---------- releases ----------

func (c *Client) ListReleases() ([]*model.Release, error) {
	resp, err := c.rpc.ListReleases(context.Background(), &grpcapi.Empty{})
	if err != nil {
		return nil, err
	}
	var releases []*model.Release
	for _, pr := range resp.Releases {
		releases = append(releases, &model.Release{
			ID:        pr.Id,
			Version:   pr.Version,
			Notes:     pr.Notes,
			Path:      pr.Path,
			CreatedAt: time.Unix(pr.CreatedAtUnix, 0),
		})
	}
	return releases, nil
}

// ---------- conversations ----------

func (c *Client) ListConversations(taskID, role string, limit int) ([]*model.LLMLog, error) {
	// Conversation logs live in the local SQLite DB and are not exposed over gRPC.
	return nil, nil
}

func (c *Client) TokenStats() ([]state.RoleTokenStats, error) {
	// Token stats live in the local SQLite DB and are not exposed over gRPC.
	return nil, nil
}

// ---------- chat ----------

func (c *Client) Chat(ctx context.Context, role, message string, inject bool) (<-chan string, error) {
	stream, err := c.rpc.Chat(ctx, &grpcapi.ChatRequest{
		Role:    role,
		Message: message,
		Inject:  inject,
	})
	if err != nil {
		return nil, err
	}
	out := make(chan string, 64)
	go func() {
		defer close(out)
		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			if chunk.Done {
				return
			}
			select {
			case out <- chunk.Text:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// ---------- helpers ----------

func fromProtoTask(pt *grpcapi.Task) model.Task {
	t := model.Task{
		ID:                 pt.Id,
		Title:              pt.Title,
		Description:        pt.Description,
		AcceptanceCriteria: pt.AcceptanceCriteria,
		AssignedRole:       pt.AssignedRole,
		Status:             model.TaskStatus(pt.Status),
		RetryCount:         int(pt.RetryCount),
		ParentTaskID:       pt.ParentTaskId,
		CreatedAt:          time.Unix(pt.CreatedAtUnix, 0),
		UpdatedAt:          time.Unix(pt.UpdatedAtUnix, 0),
	}
	if pt.ConversationHistoryJson != "" {
		_ = json.Unmarshal([]byte(pt.ConversationHistoryJson), &t.ConversationHistory)
	}
	if pt.ExecutionResultsJson != "" {
		_ = json.Unmarshal([]byte(pt.ExecutionResultsJson), &t.ExecutionResults)
	}
	return t
}

func fromProtoEvent(pe *grpcapi.EventResponse) model.Event {
	var payload map[string]any
	if pe.PayloadJson != "" {
		_ = json.Unmarshal([]byte(pe.PayloadJson), &payload)
	}
	return model.Event{
		ID:        pe.Id,
		Type:      pe.Type,
		Payload:   payload,
		CreatedAt: time.Unix(pe.CreatedAtUnix, 0),
	}
}
