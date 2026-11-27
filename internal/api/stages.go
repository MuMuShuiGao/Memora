package api

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"memora/internal/period"
	"memora/internal/store"
)

// maxStageBodyChars 是阶段名的字数上限（ADR-0030），按码点计，与待办正文、心得同规。
const maxStageBodyChars = 30

// stageCreate 的 body 必填：一个没有名字的阶段在界面上没有可点的位置。
type stageCreate struct {
	Body string `json:"body"`
}

type stagePatch struct {
	Body     *string `json:"body"`
	Done     *bool   `json:"done"`
	Takeaway *string `json:"takeaway"`
}

func (s *Server) createStage(w http.ResponseWriter, r *http.Request) {
	taskID, ok := s.taskID(w, r)
	if !ok {
		return
	}
	var in stageCreate
	if !s.readJSON(w, r, &in) {
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		s.fail(w, http.StatusBadRequest, "bad_request", "阶段名不能为空")
		return
	}
	if utf8.RuneCountInString(body) > maxStageBodyChars {
		s.fail(w, http.StatusBadRequest, "bad_request", "阶段名最多 30 个字")
		return
	}

	st, err := s.st.CreateStage(r.Context(), taskID, body, s.now())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, http.StatusNotFound, "not_found", "待办不存在")
			return
		}
		s.stageError(w, r, err)
		return
	}
	s.write(w, http.StatusCreated, st)
}

// updateStage 与待办 PATCH 同形：三样东西各自可单发，什么都没带直接拒绝。
func (s *Server) updateStage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskID(w, r)
	if !ok {
		return
	}
	var in stagePatch
	if !s.readJSON(w, r, &in) {
		return
	}
	if in.Body == nil && in.Done == nil && in.Takeaway == nil {
		s.fail(w, http.StatusBadRequest, "bad_request", `PATCH 要带 {"body":"..."}、{"done":true/false} 或 {"takeaway":"..."}`)
		return
	}

	out := store.Stage{}
	if in.Body != nil {
		body := strings.TrimSpace(*in.Body)
		if body == "" {
			s.fail(w, http.StatusBadRequest, "bad_request", "阶段名不能为空")
			return
		}
		if utf8.RuneCountInString(body) > maxStageBodyChars {
			s.fail(w, http.StatusBadRequest, "bad_request", "阶段名最多 30 个字")
			return
		}
		got, err := s.st.UpdateStageBody(r.Context(), id, body, s.now())
		if err != nil {
			s.stageError(w, r, err)
			return
		}
		out = got
	}

	if in.Takeaway != nil {
		takeaway := strings.TrimSpace(*in.Takeaway)
		if utf8.RuneCountInString(takeaway) > maxTakeawayChars {
			s.fail(w, http.StatusBadRequest, "bad_request", "心得最多 50 个字")
			return
		}
		got, err := s.st.UpdateStageTakeaway(r.Context(), id, takeaway, s.now())
		if err != nil {
			s.stageError(w, r, err)
			return
		}
		out = got
	}

	// 勾阶段和勾待办同一套归属口径：按此刻所在的周/月进报表（ADR-0030）。
	if in.Done != nil {
		at := s.now()
		got, err := s.st.SetStageDone(r.Context(), id, *in.Done, at,
			period.WeekStartID(at, s.cfg.LocalTZ),
			period.MonthStartID(at, s.cfg.LocalTZ))
		if err != nil {
			s.stageError(w, r, err)
			return
		}
		out = got
	}
	s.ok(w, out)
}

func (s *Server) deleteStage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.taskID(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteStage(r.Context(), id); err != nil {
		s.stageError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stageError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.fail(w, http.StatusNotFound, "not_found", "阶段不存在")
	case errors.Is(err, store.ErrTooManyStages):
		s.fail(w, http.StatusBadRequest, "bad_request", "一条待办最多 5 个阶段")
	default:
		s.serverError(w, r, "阶段操作", err)
	}
}
