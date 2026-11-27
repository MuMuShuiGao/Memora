package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBooksCreateListOrderAndUpdates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	base := time.Date(2026, time.November, 25, 9, 0, 0, 0, time.UTC)

	oct, err := s.CreateBook(ctx, "十月的书", "作者甲", "2026-10", base)
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	novFirst, err := s.CreateBook(ctx, "十一月的书", "", "2026-11", base.Add(time.Minute))
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	novSecond, err := s.CreateBook(ctx, "同月的第二本", "", "2026-11", base.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	if oct.Takeaway != "" || oct.Author != "作者甲" {
		t.Errorf("新建书 = %+v, 期望心得为空、作者原样", oct)
	}

	// 读完月份倒序，同月里后加的在前（ADR-0029）。
	list, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(list) != 3 ||
		list[0].ID != novSecond.ID || list[1].ID != novFirst.ID || list[2].ID != oct.ID {
		t.Fatalf("顺序 = %+v, 期望 同月第二本 / 同月第一本 / 十月的书", list)
	}

	got, err := s.UpdateBookTakeaway(ctx, novFirst.ID, "读完有点失望", base.Add(time.Hour))
	if err != nil {
		t.Fatalf("UpdateBookTakeaway: %v", err)
	}
	if got.Takeaway != "读完有点失望" || got.Title != "十一月的书" {
		t.Errorf("写心得后 = %+v, 期望只动心得", got)
	}

	// 四个字段各改各的：改名不碰心得与月份，改月不碰作者。
	got, err = s.UpdateBookTitle(ctx, novFirst.ID, "改过的书名", base.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("UpdateBookTitle: %v", err)
	}
	if got.Title != "改过的书名" || got.Takeaway != "读完有点失望" || got.FinishedMonth != "2026-11" {
		t.Errorf("改名后 = %+v, 期望只动书名", got)
	}
	got, err = s.UpdateBookAuthor(ctx, novFirst.ID, "某作者", base.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("UpdateBookAuthor: %v", err)
	}
	if got.Author != "某作者" || got.Title != "改过的书名" {
		t.Errorf("改作者后 = %+v, 期望只动作者", got)
	}
	got, err = s.UpdateBookFinishedMonth(ctx, novFirst.ID, "2026-09", base.Add(4*time.Hour))
	if err != nil {
		t.Fatalf("UpdateBookFinishedMonth: %v", err)
	}
	if got.FinishedMonth != "2026-09" || got.Author != "某作者" {
		t.Errorf("改月份后 = %+v, 期望只动读完月份", got)
	}

	// 空串写回等于清掉心得，书本身还在。
	cleared, err := s.UpdateBookTakeaway(ctx, novFirst.ID, "", base.Add(5*time.Hour))
	if err != nil {
		t.Fatalf("清空心得的调用失败: %v", err)
	}
	if cleared.Takeaway != "" {
		t.Errorf("清空后心得 = %q, 期望空串", cleared.Takeaway)
	}

	if _, err := s.UpdateBookTitle(ctx, 9999, "不存在", base); !errors.Is(err, ErrNotFound) {
		t.Errorf("未知 id 改书名 = %v, 期望 ErrNotFound", err)
	}
	if _, err := s.GetBook(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Errorf("未知 id 取书 = %v, 期望 ErrNotFound", err)
	}
}

func TestBooksDelete(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, time.November, 25, 9, 0, 0, 0, time.UTC)

	book, err := s.CreateBook(ctx, "删掉的书", "", "2026-11", now)
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	if err := s.DeleteBook(ctx, book.ID); err != nil {
		t.Fatalf("DeleteBook: %v", err)
	}
	if err := s.DeleteBook(ctx, book.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("重复删除 = %v, 期望 ErrNotFound", err)
	}
	list, err := s.ListBooks(ctx)
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("删除后书单 = %+v, 期望空", list)
	}
}
