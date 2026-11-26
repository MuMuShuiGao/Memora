package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"memora/internal/period"
	"memora/internal/store"
)

// reorder 一次提交整列未完成 id。上限按 id 的 JSON 长度估：500 条 * 8 字节远在
// maxBodyBytes 之内，这条上限守的是"客户端把已完成的也塞进来"那种逻辑错。
type taskPatch struct {
	Body *string `json:"body"`
	Done *bool   `json:"done"`
}

type taskIds struct {
	IDs []int64 `json:"ids"`
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.st.ListTasks(r.Context())
	if err != nil {
		s.serverError(w, r, "待办列表", err)
		return
	}
	s.taskList(w, tasks)
}

// taskList 是 GET 和 reorder 共用的响应形状。reorder 之后照样回全量，
// 客户端拿它覆盖内存里那份乐观结果——顺序的权威永远在服务端。
func (s *Server) taskList(w http.ResponseWriter, tasks []store.Task) {
	open := 0
	for _, t := range tasks {
		if !t.Done {
			open++
		}
	}
	s.ok(w, map[string]any{
		"tasks":      tasks,
		"open_count": open,
		"done_count": len(tasks) - open,
	})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	// 长度上限与记录共用一个常量：待办是一句话，超限同样直接拒，不静默截断。
	if err := s.checkBody(body); err != nil {
		s.fail(w, statusForBody(err), codeForBody(err), err.Error())
		return
	}

	task, err := s.st.CreateTask(r.Context(), body, s.now())
	if err != nil {
		s.serverError(w, r, "新建待办", err)
		return
	}
	s.write(w, http.StatusCreated, task)
}

// updateTask 走 PATCH：改正文和改完成态是两件常做的事，客户端一次只发其中一样。
// 两个字段都缺没有意义——改顺序是 reorder 的活，不在这里。
func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskID(w, r)
	if !ok {
		return
	}
	var in taskPatch
	if !s.readJSON(w, r, &in) {
		return
	}
	if in.Body == nil && in.Done == nil {
		s.fail(w, http.StatusBadRequest, "bad_request", `PATCH 要带 {"body":"..."} 或 {"done":true/false}`)
		return
	}

	out := store.Task{}
	if in.Body != nil {
		body := strings.TrimSpace(*in.Body)
		if err := s.checkBody(body); err != nil {
			s.fail(w, statusForBody(err), codeForBody(err), err.Error())
			return
		}
		got, err := s.st.UpdateTaskBody(r.Context(), id, body, s.now())
		if err != nil {
			s.taskError(w, r, err)
			return
		}
		out = got
	}

	// 同时给了两个字段时先改正文再改完成态：勾掉靠的是这一刻的归属列，
	// 顺序反过来会让"改完文字顺手勾上"的报表归属差出一个请求的耗时。
	if in.Done != nil {
		at := s.now()
		got, err := s.st.SetTaskDone(r.Context(), id, *in.Done, at,
			period.WeekStartID(at, s.cfg.LocalTZ),
			period.MonthStartID(at, s.cfg.LocalTZ))
		if err != nil {
			s.taskError(w, r, err)
			return
		}
		out = got
	}
	s.ok(w, out)
}

func (s *Server) reorderTasks(w http.ResponseWriter, r *http.Request) {
	var in taskIds
	if !s.readJSON(w, r, &in) {
		return
	}
	if len(in.IDs) == 0 {
		s.fail(w, http.StatusBadRequest, "bad_request", "ids 不能为空数组")
		return
	}
	if _, err := s.st.ResequenceTasks(r.Context(), in.IDs, s.now()); err != nil {
		if errors.Is(err, store.ErrBadOrder) {
			// 客户端手上的列表过期了（另一台设备刚加或刚勾了一条）。这不重试也不忽略，
			// 直接把服务端原文回出来，界面上"刷新一下就好"是这条分支唯一该有的结局。
			s.fail(w, http.StatusBadRequest, "bad_request", "顺序和服务端对不上了，刷新后重拖")
			return
		}
		s.serverError(w, r, "重排待办", err)
		return
	}
	tasks, err := s.st.ListTasks(r.Context())
	if err != nil {
		s.serverError(w, r, "待办列表", err)
		return
	}
	s.taskList(w, tasks)
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskID(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteTask(r.Context(), id, s.now()); err != nil {
		s.taskError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) taskID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return s.noteID(w, r)
}

// readJSON 解一个非 noteBody 的请求体。复用 maxBodyBytes 那层防弹，
// 错误形状和 readBody 保持一致，界面上的提示文案才不会分两套。
func (s *Server) readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			s.fail(w, http.StatusRequestEntityTooLarge, "too_large", "请求体超过上限")
		case errors.Is(err, io.EOF):
			s.fail(w, http.StatusBadRequest, "bad_request", "请求体不能为空")
		default:
			s.fail(w, http.StatusBadRequest, "bad_request", "请求体要 JSON")
		}
		return false
	}
	return true
}

func (s *Server) taskError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "not_found", "待办不存在")
		return
	}
	s.serverError(w, r, "待办操作", err)
}
