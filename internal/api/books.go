package api

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"memora/internal/store"
)

// 读书清单的字段上限（ADR-0029）。心得比待办的 50 字宽：书的心得是一段完整的想法，
// 不是完成一件小事的备注。全部按码点计，与记录、待办同一口径。
const (
	maxBookTitleChars    = 100
	maxBookAuthorChars   = 100
	maxBookTakeawayChars = 200
)

const bookMonthLayout = "2006-01"

var errBadMonth = errors.New("finished_month 要 YYYY-MM，例如 2026-11")

// bookMonthID 只认带零填充的 YYYY-MM：Go 的 "2006-01" 布局要求两位月份，
// 再用 Format 回比一次，挡掉解析器可能放宽的写法。
func bookMonthID(s string) bool {
	t, err := time.Parse(bookMonthLayout, s)
	return err == nil && t.Format(bookMonthLayout) == s
}

type bookCreate struct {
	Title         string `json:"title"`
	Author        string `json:"author"`
	FinishedMonth string `json:"finished_month"`
}

// bookPatch 的字段全是指针：nil 是"没改"，空串是"清掉"（作者、心得可清，书名与年月不可清）。
type bookPatch struct {
	Title         *string `json:"title"`
	Author        *string `json:"author"`
	FinishedMonth *string `json:"finished_month"`
	Takeaway      *string `json:"takeaway"`
}

func (s *Server) listBooks(w http.ResponseWriter, r *http.Request) {
	books, err := s.st.ListBooks(r.Context())
	if err != nil {
		s.serverError(w, r, "书单", err)
		return
	}
	s.ok(w, map[string]any{"books": books, "count": len(books)})
}

func (s *Server) createBook(w http.ResponseWriter, r *http.Request) {
	var in bookCreate
	if !s.readJSON(w, r, &in) {
		return
	}
	title := strings.TrimSpace(in.Title)
	if problem := bookTitleProblem(title); problem != "" {
		s.fail(w, http.StatusBadRequest, "bad_request", problem)
		return
	}
	author := strings.TrimSpace(in.Author)
	if utf8.RuneCountInString(author) > maxBookAuthorChars {
		s.fail(w, http.StatusBadRequest, "bad_request", "作者最多 100 个字")
		return
	}
	month, err := s.monthOrNow(in.FinishedMonth)
	if err != nil {
		s.fail(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	book, err := s.st.CreateBook(r.Context(), title, author, month, s.now())
	if err != nil {
		s.serverError(w, r, "新建书", err)
		return
	}
	s.write(w, http.StatusCreated, book)
}

func (s *Server) updateBook(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bookID(w, r)
	if !ok {
		return
	}
	var in bookPatch
	if !s.readJSON(w, r, &in) {
		return
	}
	if in.Title == nil && in.Author == nil && in.FinishedMonth == nil && in.Takeaway == nil {
		s.fail(w, http.StatusBadRequest, "bad_request",
			`PATCH 要带 {"title":"..."}、{"author":"..."}、{"finished_month":"YYYY-MM"} 或 {"takeaway":"..."}`)
		return
	}

	out := store.Book{}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if problem := bookTitleProblem(title); problem != "" {
			s.fail(w, http.StatusBadRequest, "bad_request", problem)
			return
		}
		got, err := s.st.UpdateBookTitle(r.Context(), id, title, s.now())
		if err != nil {
			s.bookError(w, r, err)
			return
		}
		out = got
	}
	if in.Author != nil {
		author := strings.TrimSpace(*in.Author)
		if utf8.RuneCountInString(author) > maxBookAuthorChars {
			s.fail(w, http.StatusBadRequest, "bad_request", "作者最多 100 个字")
			return
		}
		got, err := s.st.UpdateBookAuthor(r.Context(), id, author, s.now())
		if err != nil {
			s.bookError(w, r, err)
			return
		}
		out = got
	}
	// 读完月份只能换到另一个年月，不接受清空：界面上没有"月份待定"这个状态。
	if in.FinishedMonth != nil {
		month := strings.TrimSpace(*in.FinishedMonth)
		if !bookMonthID(month) {
			s.fail(w, http.StatusBadRequest, "bad_request", "finished_month 要 YYYY-MM，且不能清空")
			return
		}
		got, err := s.st.UpdateBookFinishedMonth(r.Context(), id, month, s.now())
		if err != nil {
			s.bookError(w, r, err)
			return
		}
		out = got
	}
	// 心得可以写也可以清：空串就是清掉（ADR-0029）。
	if in.Takeaway != nil {
		takeaway := strings.TrimSpace(*in.Takeaway)
		if utf8.RuneCountInString(takeaway) > maxBookTakeawayChars {
			s.fail(w, http.StatusBadRequest, "bad_request", "心得最多 200 个字")
			return
		}
		got, err := s.st.UpdateBookTakeaway(r.Context(), id, takeaway, s.now())
		if err != nil {
			s.bookError(w, r, err)
			return
		}
		out = got
	}
	s.ok(w, out)
}

func (s *Server) deleteBook(w http.ResponseWriter, r *http.Request) {
	id, ok := s.bookID(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteBook(r.Context(), id); err != nil {
		s.bookError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) bookID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return s.noteID(w, r)
}

// monthOrNow 规范创建请求里的读完月份：空值落服务端本地时区的当月（客户端的添加口
// 永远显式发手机当月，这个缺省只照顾 curl 与旧客户端，口径与 due_on 一致）。
func (s *Server) monthOrNow(raw string) (string, error) {
	month := strings.TrimSpace(raw)
	if month == "" {
		return s.now().In(s.cfg.LocalTZ).Format(bookMonthLayout), nil
	}
	if !bookMonthID(month) {
		return "", errBadMonth
	}
	return month, nil
}

func bookTitleProblem(title string) string {
	if title == "" {
		return "书名不能为空"
	}
	if utf8.RuneCountInString(title) > maxBookTitleChars {
		return "书名最多 100 个字"
	}
	return ""
}

func (s *Server) bookError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		s.fail(w, http.StatusNotFound, "not_found", "书不存在")
		return
	}
	s.serverError(w, r, "读书操作", err)
}
