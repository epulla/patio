package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
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

func TestSweepHandoffTimeout(t *testing.T) {
	s, parent, child, _ := newHandoffChats(t)
	queueHandoff(t, s, parent, child, true)
	if _, err := s.db.Exec(`UPDATE chat_handoff SET created_at='2020-01-01T00:00:00.000Z',deadline_at='2020-01-01T00:01:00.000Z' WHERE from_chat_id=?`, parent); err != nil {
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
	if status != "failed" || !strings.Contains(result, "Timed out") {
		t.Fatalf("status=%s result=%s", status, result)
	}
	if !strings.Contains(report, "Timed out") {
		t.Fatalf("report=%q", report)
	}
	var notices int
	if err := s.db.QueryRow(`SELECT count(*) FROM notification WHERE title LIKE 'Handoff timed out:%' AND link=?`, "/ai/chats/"+child).Scan(&notices); err != nil || notices != 1 {
		t.Fatalf("notifications=%d err=%v", notices, err)
	}
}
