package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func newHandoffChats(t *testing.T) (*Server, string, string, string) {
	t.Helper()
	db, _, root := chatAPI(t)
	connector := createTestConnector(t, db)
	a := createDriverTestChat(t, db, connector, root)
	b := createDriverTestChat(t, db, connector, root)
	c := createDriverTestChat(t, db, connector, root)
	s := New(db, "local", false, "dev", root)
	t.Cleanup(func() { _ = s.Close() })
	return s, a, b, c
}

func queueHandoff(t *testing.T, s *Server, parent, child string, report bool) {
	t.Helper()
	code := requestJSON(t, s, http.MethodPost, "/api/chats/"+child+"/agent-messages", map[string]any{"from_chat_id": parent, "text": "work", "report_back": report}, nil)
	if code != 200 {
		t.Fatalf("queue status=%d", code)
	}
}

func TestAgentMessageCreatesOnlyRequestedHandoff(t *testing.T) {
	s, a, b, c := newHandoffChats(t)
	queueHandoff(t, s, a, b, true)
	queueHandoff(t, s, a, c, false)
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM chat_handoff WHERE from_chat_id=? AND status='pending'`, a).Scan(&n); err != nil || n != 1 {
		t.Fatalf("pending handoffs=%d err=%v", n, err)
	}
}

func TestRepeatedReportBackReusesPendingHandoff(t *testing.T) {
	s, a, b, _ := newHandoffChats(t)
	queueHandoff(t, s, a, b, true)
	queueHandoff(t, s, a, b, true)
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM chat_handoff WHERE from_chat_id=? AND to_chat_id=?`, a, b).Scan(&n); err != nil || n != 1 {
		t.Fatalf("handoffs=%d err=%v", n, err)
	}
	parents, _, err := s.chats.finishHandoffs(b, "done", nil, false)
	if err != nil || len(parents) != 1 {
		t.Fatalf("finish %v %v", parents, err)
	}
	queueHandoff(t, s, a, b, true)
	if err := s.db.QueryRow(`SELECT count(*) FROM chat_handoff WHERE from_chat_id=? AND to_chat_id=?`, a, b).Scan(&n); err != nil || n != 2 {
		t.Fatalf("handoffs after finish=%d err=%v", n, err)
	}
}

func TestHandoffDeferredUntilOwnHandoffsFinish(t *testing.T) {
	s, parent, child, grandchild := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	queueHandoff(t, s, child, grandchild, true)
	var sent []string
	s.chats.reportBack = func(to, from, text string) error { sent = append(sent, to+":"+text); return nil }
	parents, tracked, err := s.chats.finishHandoffs(child, "child result", nil, false)
	if err != nil || !tracked || len(parents) != 0 {
		t.Fatalf("child finish %v %v %v", parents, tracked, err)
	}
	if err := s.chats.deliverHandoffReports(parent); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 0 {
		t.Fatalf("premature report: %v", sent)
	}
	parents, _, err = s.chats.finishHandoffs(grandchild, "grandchild result", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		if err := s.chats.deliverHandoffReports(p); err != nil {
			t.Fatal(err)
		}
	}
	parents, _, err = s.chats.finishHandoffs(child, "child final", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		if err := s.chats.deliverHandoffReports(p); err != nil {
			t.Fatal(err)
		}
	}
	if len(sent) != 2 {
		t.Fatalf("reports=%v", sent)
	}
}

func TestMutualHandoffsReportWithoutDeadlock(t *testing.T) {
	s, implementer, reviewer, _ := newHandoffChats(t)
	queueHandoff(t, s, implementer, reviewer, true)
	queueHandoff(t, s, reviewer, implementer, true)
	var sent []string
	s.chats.reportBack = func(to, from, text string) error { sent = append(sent, to+":"+text); return nil }
	finish := func(chat, result string) {
		t.Helper()
		parents, _, err := s.chats.finishHandoffs(chat, result, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range parents {
			if err := s.chats.deliverHandoffReports(p); err != nil {
				t.Fatal(err)
			}
		}
	}
	finish(implementer, "fixes done")
	if len(sent) != 1 || !strings.HasPrefix(sent[0], reviewer+":") {
		t.Fatalf("implementer report=%v", sent)
	}
	finish(reviewer, "approved")
	if len(sent) != 2 || !strings.HasPrefix(sent[1], implementer+":") {
		t.Fatalf("reviewer report=%v", sent)
	}
}

func TestHandoffCombinedReportsWaitForAllChildren(t *testing.T) {
	s, parent, first, second := newHandoffChats(t)
	for id, title := range map[string]string{first: "First", second: "Second"} {
		if _, err := s.db.Exec(`UPDATE chat SET title=? WHERE id=?`, title, id); err != nil {
			t.Fatal(err)
		}
	}
	queueHandoff(t, s, parent, first, true)
	queueHandoff(t, s, parent, second, true)
	var got []string
	s.chats.reportBack = func(_, _, text string) error { got = append(got, text); return nil }
	parents, _, err := s.chats.finishHandoffs(first, "one", errors.New("bad"), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		if err := s.chats.deliverHandoffReports(p); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 0 {
		t.Fatalf("early report %v", got)
	}
	parents, _, err = s.chats.finishHandoffs(second, "two", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range parents {
		if err := s.chats.deliverHandoffReports(p); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 1 || !strings.Contains(got[0], "Reports from 2 handoffs:") || !strings.Contains(got[0], "First (failed)") || !strings.Contains(got[0], "Second (ok)") {
		t.Fatalf("combined=%v", got)
	}
}

func TestDiscardedQueuedHandoffReleasesParentReports(t *testing.T) {
	s, parent, discarded, finished := newHandoffChats(t)
	queueHandoff(t, s, parent, discarded, true)
	queueHandoff(t, s, parent, finished, true)
	var got []string
	s.chats.reportBack = func(_, _, text string) error { got = append(got, text); return nil }
	if _, _, err := s.chats.finishHandoffs(finished, "done", nil, false); err != nil {
		t.Fatal(err)
	}
	var qid string
	if err := s.db.QueryRow(`SELECT id FROM chat_queue WHERE chat_id=?`, discarded).Scan(&qid); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, s, http.MethodDelete, "/api/chats/"+discarded+"/queue/"+qid, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status=%d", code)
	}
	if len(got) != 1 || !strings.Contains(got[0], "(stopped)") || !strings.Contains(got[0], "(ok)") {
		t.Fatalf("reports=%v", got)
	}
}

func TestDiscardWaitsForConcurrentDeliveryAndKeepsHandoff(t *testing.T) {
	s, parent, child, _ := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	queueHandoff(t, s, parent, child, true)
	var ids []string
	rows, err := s.db.Query(`SELECT id FROM chat_queue WHERE chat_id=? ORDER BY rowid`, child)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil || len(ids) != 2 {
		t.Fatalf("queued=%v err=%v", ids, err)
	}
	// Mimic drainQueuedMode: under the queue lock the claimed row is gone before its chat_message exists.
	lock := s.chats.queueLock(child)
	lock.Lock()
	if _, err := s.db.Exec(`DELETE FROM chat_queue WHERE id=?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	done := make(chan int)
	go func() {
		done <- requestJSON(t, s, http.MethodDelete, "/api/chats/"+child+"/queue/"+ids[1], nil, nil)
	}()
	select {
	case code := <-done:
		t.Fatalf("delete finished during delivery: %d", code)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := insertChatMessage(s.db, child, "user", "work", nil, &parent, &parent); err != nil {
		t.Fatal(err)
	}
	lock.Unlock()
	if code := <-done; code != http.StatusNoContent {
		t.Fatalf("delete status=%d", code)
	}
	var status string
	if err := s.db.QueryRow(`SELECT status FROM chat_handoff WHERE from_chat_id=? AND to_chat_id=?`, parent, child).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("status=%q err=%v", status, err)
	}
}

func TestDiscardKeepsQueuedMessageWhenHandoffStopFails(t *testing.T) {
	s, parent, child, _ := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_handoff BEFORE UPDATE ON chat_handoff BEGIN SELECT RAISE(ABORT,'boom'); END`); err != nil {
		t.Fatal(err)
	}
	var qid string
	if err := s.db.QueryRow(`SELECT id FROM chat_queue WHERE chat_id=?`, child).Scan(&qid); err != nil {
		t.Fatal(err)
	}
	if code := requestJSON(t, s, http.MethodDelete, "/api/chats/"+child+"/queue/"+qid, nil, nil); code != http.StatusInternalServerError {
		t.Fatalf("delete status=%d", code)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM chat_queue WHERE id=?`, qid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("queued=%d err=%v", n, err)
	}
}

func TestReplaceDropsOwnQueuedMessages(t *testing.T) {
	s, parent, child, other := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	queueHandoff(t, s, parent, child, true)
	queueHandoff(t, s, other, child, false)
	queueHandoff(t, s, parent, other, true)
	var got []string
	var mu sync.Mutex
	s.chats.reportBack = func(_, _, text string) error { mu.Lock(); got = append(got, text); mu.Unlock(); return nil }
	if _, _, err := s.chats.finishHandoffs(other, "done", nil, false); err != nil {
		t.Fatal(err)
	}
	replace := func(report bool) {
		t.Helper()
		if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+child+"/agent-messages", map[string]any{"from_chat_id": parent, "text": "newer", "report_back": report, "replace": true}, nil); code != 200 {
			t.Fatalf("replace status=%d", code)
		}
		var mine, others int
		if err := s.db.QueryRow(`SELECT count(*) FILTER (WHERE from_chat_id=?),count(*) FILTER (WHERE from_chat_id=?) FROM chat_queue WHERE chat_id=?`, parent, other, child).Scan(&mine, &others); err != nil || mine != 1 || others != 1 {
			t.Fatalf("queued mine=%d others=%d err=%v", mine, others, err)
		}
	}
	status := func() string {
		var st string
		if err := s.db.QueryRow(`SELECT status FROM chat_handoff WHERE from_chat_id=? AND to_chat_id=?`, parent, child).Scan(&st); err != nil {
			t.Fatal(err)
		}
		return st
	}
	replace(true)
	if st := status(); st != "pending" {
		t.Fatalf("status after report_back replace=%s", st)
	}
	replace(false)
	if st := status(); st != "stopped" {
		t.Fatalf("status after plain replace=%s", st)
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 1 && strings.Contains(got[0], "(stopped)") })
}

func TestHandoffStatusLabelForSingleReport(t *testing.T) {
	for _, tc := range []struct {
		stopped bool
		failure bool
		label   string
	}{{failure: true, label: "[Status: failed]"}, {stopped: true, label: "[Status: stopped]"}} {
		s, parent, child, _ := newHandoffChats(t)
		queueHandoff(t, s, parent, child, true)
		var got string
		s.chats.reportBack = func(_, _, text string) error { got = text; return nil }
		var e error
		if tc.failure {
			e = errors.New("failure")
		}
		parents, _, err := s.chats.finishHandoffs(child, "result", e, tc.stopped)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range parents {
			if err := s.chats.deliverHandoffReports(p); err != nil {
				t.Fatal(err)
			}
		}
		if !strings.HasPrefix(got, tc.label) {
			t.Fatalf("report=%q", got)
		}
	}
}

func TestSweepHandoffTimesOutOnlyIdleChildren(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active func(s *Server, child, other string)
	}{
		{"recent message", func(s *Server, child, _ string) {
			if _, err := s.db.Exec(`UPDATE chat SET updated_at=? WHERE id=?`, now(), child); err != nil {
				t.Fatal(err)
			}
		}},
		{"running turn", func(s *Server, child, _ string) { s.chats.runtime(child).busy.Store(true) }},
		{"subagent event", func(s *Server, child, _ string) { s.chats.subagentActivity.Store(child, time.Now()) }},
		{"waiting on own handoff", func(s *Server, child, other string) { queueHandoff(t, s, child, other, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, parent, child, other := newHandoffChats(t)
			queueHandoff(t, s, parent, child, true)
			if _, err := s.db.Exec(`UPDATE chat_handoff SET created_at='2020-01-01T00:00:00.000Z',deadline_at='2020-01-01T00:01:00.000Z' WHERE from_chat_id=?`, parent); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE chat SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, child); err != nil {
				t.Fatal(err)
			}
			tc.active(s, child, other)
			if err := s.SweepHandoffs(context.Background()); err != nil {
				t.Fatal(err)
			}
			var status string
			if err := s.db.QueryRow(`SELECT status FROM chat_handoff WHERE from_chat_id=?`, parent).Scan(&status); err != nil || status != "pending" {
				t.Fatalf("status=%s err=%v", status, err)
			}
		})
	}
}

func TestSweepHandoffTimesOutIdleCycles(t *testing.T) {
	for _, size := range []int{2, 3} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			s, a, b, c := newHandoffChats(t)
			chats := []string{a, b, c}[:size]
			for i, from := range chats {
				queueHandoff(t, s, from, chats[(i+1)%size], true)
			}
			if _, err := s.db.Exec(`UPDATE chat_handoff SET created_at='2020-01-01T00:00:00.000Z',deadline_at='2020-01-01T00:01:00.000Z'`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE chat SET updated_at='2020-01-01T00:00:00.000Z'`); err != nil {
				t.Fatal(err)
			}
			s.chats.reportBack = func(_, _, _ string) error { return nil }
			if err := s.SweepHandoffs(context.Background()); err != nil {
				t.Fatal(err)
			}
			var pending int
			if err := s.db.QueryRow(`SELECT count(*) FROM chat_handoff WHERE status='pending'`).Scan(&pending); err != nil || pending != 0 {
				t.Fatalf("pending=%d err=%v", pending, err)
			}
		})
	}
}

func TestSweepHandoffTimeout(t *testing.T) {
	s, parent, child, _ := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	if _, err := s.db.Exec(`UPDATE chat_handoff SET created_at='2020-01-01T00:00:00.000Z',deadline_at='2020-01-01T00:01:00.000Z' WHERE from_chat_id=?`, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE chat SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, child); err != nil {
		t.Fatal(err)
	}
	var report string
	s.chats.reportBack = func(_, _, text string) error { report = text; return nil }
	if err := s.SweepHandoffs(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status, result string
	if err := s.db.QueryRow(`SELECT status,result FROM chat_handoff WHERE from_chat_id=?`, parent).Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || result != "No activity for 1 min" {
		t.Fatalf("status=%s result=%s", status, result)
	}
	if !strings.Contains(report, "No activity for 1 min") {
		t.Fatalf("report=%q", report)
	}
	var notices int
	if err := s.db.QueryRow(`SELECT count(*) FROM notification WHERE title LIKE 'Handoff timed out:%' AND link=?`, "/ai/chats/"+child).Scan(&notices); err != nil || notices != 1 {
		t.Fatalf("notifications=%d err=%v", notices, err)
	}
}

func TestHoldHandoffNeedsPendingHandoff(t *testing.T) {
	s, parent, child, other := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+other+"/handoff-hold", nil, nil); code != 409 {
		t.Fatalf("no handoff status=%d", code)
	}
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+child+"/handoff-hold", nil, nil); code != 200 {
		t.Fatalf("hold status=%d", code)
	}
	var held int
	if err := s.db.QueryRow(`SELECT held FROM chat_handoff WHERE to_chat_id=?`, child).Scan(&held); err != nil || held != 1 {
		t.Fatalf("held=%d err=%v", held, err)
	}
}

func TestHeldTurnReportsProgressAndKeepsHandoffOpen(t *testing.T) {
	s, parent, child, _ := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	var sent []string
	s.chats.reportBack = func(to, _, text string) error { sent = append(sent, to+":"+text); return nil }
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+child+"/handoff-hold", nil, nil); code != 200 {
		t.Fatalf("hold status=%d", code)
	}
	parents, tracked, err := s.chats.finishHandoffs(child, "waiting on subagent", nil, false)
	s.chats.background.Wait()
	if err != nil || !tracked || len(parents) != 0 {
		t.Fatalf("held finish %v %v %v", parents, tracked, err)
	}
	var status string
	var held int
	if err := s.db.QueryRow(`SELECT status,held FROM chat_handoff WHERE to_chat_id=?`, child).Scan(&status, &held); err != nil || status != "pending" || held != 0 {
		t.Fatalf("status=%s held=%d err=%v", status, held, err)
	}
	parents, _, err = s.chats.finishHandoffs(child, "final result", nil, false)
	if err != nil || len(parents) != 1 {
		t.Fatalf("final finish %v %v", parents, err)
	}
	if err := s.chats.deliverHandoffReports(parent); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT status FROM chat_handoff WHERE to_chat_id=?`, child).Scan(&status); err != nil || status != "ok" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	if strings.Join(sent, "|") != parent+":[Progress] waiting on subagent|"+parent+":final result" {
		t.Fatalf("sent=%v", sent)
	}
}

func TestSweepTimesOutHeldHandoff(t *testing.T) {
	s, parent, child, _ := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	if _, err := s.db.Exec(`UPDATE chat_handoff SET held=1,created_at='2020-01-01T00:00:00.000Z',deadline_at='2020-01-01T00:01:00.000Z' WHERE to_chat_id=?`, child); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE chat SET updated_at='2020-01-01T00:00:00.000Z' WHERE id=?`, child); err != nil {
		t.Fatal(err)
	}
	s.chats.reportBack = func(_, _, _ string) error { return nil }
	if err := s.SweepHandoffs(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.db.QueryRow(`SELECT status FROM chat_handoff WHERE to_chat_id=?`, child).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

func TestHeldTurnReportsProgressWhileWaitingOnOwnHandoff(t *testing.T) {
	s, parent, child, grandchild := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	queueHandoff(t, s, child, grandchild, true)
	var sent []string
	s.chats.reportBack = func(to, _, text string) error { sent = append(sent, to+":"+text); return nil }
	if code := requestJSON(t, s, http.MethodPost, "/api/chats/"+child+"/handoff-hold", nil, nil); code != 200 {
		t.Fatalf("hold status=%d", code)
	}
	if _, _, err := s.chats.finishHandoffs(child, "waiting on review", nil, false); err != nil {
		t.Fatal(err)
	}
	s.chats.background.Wait()
	if len(sent) != 1 || sent[0] != parent+":[Progress] waiting on review" {
		t.Fatalf("sent=%v", sent)
	}
}
