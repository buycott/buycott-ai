package grpcserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"

	"buycott/internal/grpcapi"
	"buycott/internal/model"
	"buycott/internal/server"
)

// GRPCServer wraps a server.Server and exposes it via gRPC.
type GRPCServer struct {
	grpcapi.UnimplementedBuycottServiceServer
	srv  server.Server
	arts string // artifacts path for ListArtifacts
}

func New(srv server.Server, artifactsPath string) *GRPCServer {
	return &GRPCServer{srv: srv, arts: artifactsPath}
}

// Listen starts the gRPC server on the given port and blocks.
func Listen(srv server.Server, artifactsPath string, port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	s := grpc.NewServer()
	grpcapi.RegisterBuycottServiceServer(s, New(srv, artifactsPath))
	return s.Serve(lis)
}

// ---------- pipeline control ----------

func (g *GRPCServer) GetStatus(ctx context.Context, _ *grpcapi.Empty) (*grpcapi.StatusResponse, error) {
	st, err := g.srv.GetStatus()
	if err != nil {
		return nil, err
	}
	resp := &grpcapi.StatusResponse{
		Running:     st.Running,
		Paused:      st.Paused,
		QueueLength: int32(st.QueueLength),
		Completed:   int32(st.Completed),
		Escalated:   int32(st.Escalated),
	}
	if st.ActiveTask != nil {
		resp.ActiveTask = toProtoTask(st.ActiveTask)
	}
	return resp, nil
}

func (g *GRPCServer) Pause(ctx context.Context, _ *grpcapi.Empty) (*grpcapi.Empty, error) {
	return &grpcapi.Empty{}, g.srv.Pause()
}

func (g *GRPCServer) Resume(ctx context.Context, _ *grpcapi.Empty) (*grpcapi.Empty, error) {
	return &grpcapi.Empty{}, g.srv.Resume()
}

// ---------- tasks ----------

func (g *GRPCServer) ListTasks(ctx context.Context, req *grpcapi.ListTasksRequest) (*grpcapi.ListTasksResponse, error) {
	var filter model.TaskFilter
	if req.StatusFilter != "" {
		filter.Status = model.TaskStatus(req.StatusFilter)
	}
	tasks, err := g.srv.ListTasks(filter)
	if err != nil {
		return nil, err
	}
	resp := &grpcapi.ListTasksResponse{}
	for _, t := range tasks {
		resp.Tasks = append(resp.Tasks, toProtoTask(t))
	}
	return resp, nil
}

func (g *GRPCServer) GetTask(ctx context.Context, req *grpcapi.GetTaskRequest) (*grpcapi.TaskResponse, error) {
	t, err := g.srv.GetTask(req.Id)
	if err != nil {
		return nil, err
	}
	return &grpcapi.TaskResponse{Task: toProtoTask(t)}, nil
}

// ---------- events ----------

func (g *GRPCServer) ListEvents(ctx context.Context, req *grpcapi.ListEventsRequest) (*grpcapi.ListEventsResponse, error) {
	events, err := g.srv.ListEvents(int(req.Limit))
	if err != nil {
		return nil, err
	}
	resp := &grpcapi.ListEventsResponse{}
	for _, e := range events {
		resp.Events = append(resp.Events, toProtoEvent(e))
	}
	return resp, nil
}

func (g *GRPCServer) StreamEvents(_ *grpcapi.Empty, stream grpc.ServerStreamingServer[grpcapi.EventResponse]) error {
	ch, err := g.srv.StreamEvents(stream.Context())
	if err != nil {
		return err
	}
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(toProtoEvent(&ev)); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// ---------- releases ----------

func (g *GRPCServer) ListReleases(ctx context.Context, _ *grpcapi.Empty) (*grpcapi.ListReleasesResponse, error) {
	releases, err := g.srv.ListReleases()
	if err != nil {
		return nil, err
	}
	resp := &grpcapi.ListReleasesResponse{}
	for _, r := range releases {
		resp.Releases = append(resp.Releases, &grpcapi.ReleaseResponse{
			Id:            r.ID,
			Version:       r.Version,
			Notes:         r.Notes,
			Path:          r.Path,
			CreatedAtUnix: r.CreatedAt.Unix(),
		})
	}
	return resp, nil
}

// ---------- token stats ----------

func (g *GRPCServer) TokenStats(ctx context.Context, _ *grpcapi.Empty) (*grpcapi.TokenStatsResponse, error) {
	stats, err := g.srv.TokenStats()
	if err != nil {
		return nil, err
	}
	resp := &grpcapi.TokenStatsResponse{}
	for _, s := range stats {
		resp.Stats = append(resp.Stats, &grpcapi.RoleTokenStat{
			Role:         s.Role,
			Model:        s.Model,
			InputTokens:  s.InputTokens,
			OutputTokens: s.OutputTokens,
			Calls:        s.Calls,
			EstCostUsd:   s.EstCostUSD,
		})
	}
	return resp, nil
}

// ---------- conversations ----------

func (g *GRPCServer) ListConversations(ctx context.Context, req *grpcapi.ListConversationsRequest) (*grpcapi.ListConversationsResponse, error) {
	logs, err := g.srv.ListConversations(req.TaskId, req.Role, int(req.Limit))
	if err != nil {
		return nil, err
	}
	resp := &grpcapi.ListConversationsResponse{}
	for _, l := range logs {
		msgsJSON, _ := json.Marshal(l.Messages)
		resp.Logs = append(resp.Logs, &grpcapi.LLMLogResponse{
			Id:            l.ID,
			TaskId:        l.TaskID,
			Role:          l.Role,
			Model:         l.Model,
			CallType:      l.CallType,
			MessagesJson:  string(msgsJSON),
			Response:      l.Response,
			InputTokens:   int64(l.InputTokens),
			OutputTokens:  int64(l.OutputTokens),
			DurationMs:    l.DurationMs,
			CreatedAtUnix: l.CreatedAt.Unix(),
		})
	}
	return resp, nil
}

// ---------- artifacts ----------

func (g *GRPCServer) ListArtifacts(ctx context.Context, req *grpcapi.ListArtifactsRequest) (*grpcapi.ListArtifactsResponse, error) {
	base := g.arts
	if req.Subpath != "" {
		base = filepath.Join(base, filepath.Clean(req.Subpath))
	}
	resp := &grpcapi.ListArtifactsResponse{}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, fmt.Errorf("read artifacts dir: %w", err)
	}
	for _, e := range entries {
		info, _ := e.Info()
		var size int64
		if info != nil && !e.IsDir() {
			size = info.Size()
		}
		relPath := e.Name()
		if req.Subpath != "" {
			relPath = filepath.Join(req.Subpath, e.Name())
		}
		resp.Entries = append(resp.Entries, &grpcapi.ArtifactEntry{
			Path:  relPath,
			IsDir: e.IsDir(),
			Size:  size,
		})
	}
	return resp, nil
}

// ---------- chat ----------

func (g *GRPCServer) Chat(req *grpcapi.ChatRequest, stream grpc.ServerStreamingServer[grpcapi.ChatChunk]) error {
	ch, err := g.srv.Chat(stream.Context(), req.Role, req.Message, req.Inject)
	if err != nil {
		return err
	}
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return stream.Send(&grpcapi.ChatChunk{Done: true})
			}
			if err := stream.Send(&grpcapi.ChatChunk{Text: chunk}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// ---------- helpers ----------

func toProtoTask(t *model.Task) *grpcapi.Task {
	histJSON, _ := json.Marshal(t.ConversationHistory)
	execJSON, _ := json.Marshal(t.ExecutionResults)
	return &grpcapi.Task{
		Id:                      t.ID,
		Title:                   t.Title,
		Description:             t.Description,
		AcceptanceCriteria:      t.AcceptanceCriteria,
		AssignedRole:            t.AssignedRole,
		Status:                  string(t.Status),
		RetryCount:              int32(t.RetryCount),
		ParentTaskId:            t.ParentTaskID,
		CreatedAtUnix:           t.CreatedAt.Unix(),
		UpdatedAtUnix:           t.UpdatedAt.Unix(),
		ConversationHistoryJson: string(histJSON),
		ExecutionResultsJson:    string(execJSON),
	}
}

func toProtoEvent(e *model.Event) *grpcapi.EventResponse {
	payJSON, _ := json.Marshal(e.Payload)
	return &grpcapi.EventResponse{
		Id:            e.ID,
		Type:          e.Type,
		PayloadJson:   string(payJSON),
		CreatedAtUnix: e.CreatedAt.Unix(),
	}
}
