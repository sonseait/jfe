package server

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"jfe/backend/internal/route"
	"jfe/backend/internal/store"
)

type JobHistoryQuery struct {
	RunID  string `json:"runId,omitempty" jsonschema:"format=uuid"`
	Cursor string `json:"cursor,omitempty" jsonschema:"pattern=^[0-9]+$,maxLength=19"`
}
type JobRunDTO struct {
	ID          string     `json:"id"`
	Attempt     int        `json:"attempt"`
	WorkerID    string     `json:"workerId"`
	StartedAt   time.Time  `json:"startedAt"`
	FinishedAt  *time.Time `json:"finishedAt"`
	State       string     `json:"state"`
	DroppedLogs int        `json:"droppedLogs"`
}
type JobLogDTO struct {
	ID       string            `json:"id"`
	At       time.Time         `json:"at"`
	Level    string            `json:"level"`
	Message  string            `json:"message"`
	Stage    string            `json:"stage"`
	Error    string            `json:"error"`
	Filename string            `json:"filename"`
	Details  map[string]string `json:"details"`
}
type JobHistoryDTO struct {
	Runs       []JobRunDTO `json:"runs"`
	RunID      string      `json:"runId"`
	Items      []JobLogDTO `json:"items"`
	NextCursor string      `json:"nextCursor"`
}

func (s *Server) jobHistoryRoutes() {
	route.Get(s.Routes, op("/admin/jobs/:id/history", "getJobHistory", "admin"), func(ctx context.Context, in route.Input[route.Empty, JobHistoryQuery, IDParams]) (route.Output[JobHistoryDTO], error) {
		v := JobHistoryDTO{Runs: []JobRunDTO{}, Items: []JobLogDTO{}}
		if _, e := s.DB.GetJob(ctx, in.Params.ID); e != nil {
			return out(v, e)
		}
		rows, e := s.DB.ListJobRuns(ctx, in.Params.ID)
		if e != nil {
			return out(v, e)
		}
		for _, r := range rows {
			run := JobRunDTO{ID: r.ID, Attempt: int(r.Attempt), WorkerID: r.WorkerID, StartedAt: r.StartedAt, State: r.State, DroppedLogs: int(r.DroppedLogs)}
			if r.FinishedAt.Valid {
				run.FinishedAt = &r.FinishedAt.Time
			}
			v.Runs = append(v.Runs, run)
		}
		v.RunID = in.Query.RunID
		if v.RunID == "" && len(v.Runs) > 0 {
			v.RunID = v.Runs[0].ID
		}
		if v.RunID == "" {
			return out(v, nil)
		}
		found := false
		for _, r := range v.Runs {
			if r.ID == v.RunID {
				found = true
				break
			}
		}
		if !found {
			return out(v, route.Fail(404, "Job execution not found"))
		}
		var before int64
		if in.Query.Cursor != "" {
			before, e = strconv.ParseInt(in.Query.Cursor, 10, 64)
			if e != nil {
				return out(v, route.Fail(422, "Invalid log cursor"))
			}
		}
		logs, e := s.DB.ListJobLogs(ctx, store.ListJobLogsParams{JobID: in.Params.ID, RunID: v.RunID, BeforeID: before})
		if e != nil {
			return out(v, e)
		}
		if len(logs) > 200 {
			logs = logs[:200]
			v.NextCursor = strconv.FormatInt(logs[199].ID, 10)
		}
		for _, entry := range logs {
			var fields map[string]json.RawMessage
			if e = json.Unmarshal(entry.Event, &fields); e != nil {
				return out(v, e)
			}
			item := JobLogDTO{ID: strconv.FormatInt(entry.ID, 10), At: entry.CreatedAt, Details: map[string]string{}}
			_ = json.Unmarshal(fields["time"], &item.At)
			for key, value := range fields {
				switch key {
				case "time":
				case "level":
					_ = json.Unmarshal(value, &item.Level)
				case "message":
					_ = json.Unmarshal(value, &item.Message)
				case "stage":
					_ = json.Unmarshal(value, &item.Stage)
				case "error":
					_ = json.Unmarshal(value, &item.Error)
				case "filename":
					_ = json.Unmarshal(value, &item.Filename)
				case "jobId", "attempt", "workerId", "workerRole", "service", "pid":
				default:
					var text string
					if json.Unmarshal(value, &text) != nil {
						text = string(value)
					}
					item.Details[key] = text
				}
			}
			v.Items = append(v.Items, item)
		}
		return out(v, nil)
	})
}
