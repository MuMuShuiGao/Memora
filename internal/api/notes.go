package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"memora/internal/period"
	"memora/internal/store"
)

// 请求体上限按字符数换算：正文允许中文，一个字最多 4 字节，再留出 JSON 包装的余量。
// 这个上限只是防弹，真正的业务限制是 cfg.MaxNoteChars（字符数）。
const maxBodyBytes = 16 * 1024

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

type noteBody struct {
	Body string `json:"body"`
}

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := defaultListLimit
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			s.fail(w, http.StatusBadRequest, "bad_request", "limit 要是正整数")
			return
		}
		if n > maxListLimit {
			n = maxListLimit
		}
		limit = n
	}

	var before *time.Time
	if raw := q.Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			s.fail(w, http.StatusBadRequest, "bad_request", "before 要 RFC3339，例如 2026-11-24T00:00:00Z")
			return
		}
		before = &t
	}

	notes, err := s.st.ListNotes(r.Context(), q.Get("q"), before, limit)
	if err != nil {
		s.serverError(w, r, "列表查询", err)
		return
	}
	s.ok(w, map[string]any{"notes": notes, "count": len(notes)})
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	if err := s.checkBody(body); err != nil {
		s.fail(w, statusForBody(err), codeForBody(err), err.Error())
		return
	}

	// 用 s.now 而不是 time.Now：记录归属和 GET /v1/periods/now 必须来自同一个钟，
	// 否则在跨周期的那一秒里，客户端刚记下的记录会跟接口给的周期对不上。
	now := s.now()
	created, err := s.st.CreateNote(r.Context(), body, now,
		period.WeekStartID(now, s.cfg.LocalTZ),
		period.MonthStartID(now, s.cfg.LocalTZ))
	if err != nil {
		s.serverError(w, r, "新建记录", err)
		return
	}
	s.write(w, http.StatusCreated, created)
}

func (s *Server) getNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.noteID(w, r)
	if !ok {
		return
	}
	n, err := s.st.GetNote(r.Context(), id)
	if err != nil {
		s.noteError(w, r, err)
		return
	}
	s.ok(w, n)
}

func (s *Server) updateNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.noteID(w, r)
	if !ok {
		return
	}
	body, ok := s.readBody(w, r)
	if !ok {
		return
	}
	if err := s.checkBody(body); err != nil {
		s.fail(w, statusForBody(err), codeForBody(err), err.Error())
		return
	}

	// 归属两列不重算：改一条旧记录不该把它搬到当前周期去。
	n, err := s.st.UpdateNote(r.Context(), id, body, s.now())
	if err != nil {
		s.noteError(w, r, err)
		return
	}
	s.ok(w, n)
}

func (s *Server) deleteNote(w http.ResponseWriter, r *http.Request) {
	id, ok := s.noteID(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteNote(r.Context(), id); err != nil {
		s.noteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) noteID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		s.fail(w, http.StatusBadRequest, "bad_request", "id 要是正整数")
		return 0, false
	}
	return id, true
}

// readBody 解析 {"body": "..."} 并首尾去空白。返回的第二个值是"是否已经写完响应"。
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	var in noteBody
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	if err := dec.Decode(&in); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			s.fail(w, http.StatusRequestEntityTooLarge, "too_large", "请求体超过上限")
		case errors.Is(err, io.EOF):
			s.fail(w, http.StatusBadRequest, "bad_request", "请求体不能为空")
		default:
			s.fail(w, http.StatusBadRequest, "bad_request", "请求体要 JSON，形如 {\"body\":\"...\"}")
		}
		return "", false
	}
	return strings.TrimSpace(in.Body), true
}

var (
	errEmptyBody   = errors.New("正文不能为空")
	errTooLongBody = errors.New("正文超过长度上限")
)

// checkBody 超限一律拒绝，绝不静默截断——截断会让用户以为记下了而其实没有。
func (s *Server) checkBody(body string) error {
	if body == "" {
		return errEmptyBody
	}
	if utf8.RuneCountInString(body) > s.cfg.MaxNoteChars {
		return errTooLongBody
	}
	return nil
}

func statusForBody(err error) int {
	if errors.Is(err, errTooLongBody) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}

func codeForBody(err error) string {
	if errors.Is(err, errTooLongBody) {
		return "too_large"
	}
	return "bad_request"
}

func (s *Server) noteError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "not_found", "记录不存在")
		return
	}
	s.serverError(w, r, "记录操作", err)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.log.Error(what, "err", err.Error(), "path", r.URL.Path)
	// 对外不回显内部错误，避免把库结构一路带到客户端
	s.fail(w, http.StatusInternalServerError, "internal", "服务端错误")
}
