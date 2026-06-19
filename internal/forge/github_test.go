package forge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/husqylabs/stack/internal/stack"
)

// newTestGH returns a GitHub client pointed at a stub server.
func newTestGH(srv *httptest.Server) *GitHub {
	return &GitHub{Owner: "o", Repo: "r", http: srv.Client(), base: srv.URL}
}

func TestFindPR_NoneVsExisting(t *testing.T) {
	var headParam string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headParam = r.URL.Query().Get("head")
		if headParam == "o:absent" {
			io.WriteString(w, `[]`)
			return
		}
		io.WriteString(w, `[{"number":42,"state":"open","base":{"ref":"main"},"head":{"ref":"present"}}]`)
	}))
	defer srv.Close()
	gh := newTestGH(srv)

	got, err := gh.FindPR(context.Background(), "absent")
	if err != nil || got != nil {
		t.Fatalf("expected no PR, got %+v err=%v", got, err)
	}
	if headParam != "o:absent" {
		t.Fatalf("head query not owner-qualified: %q", headParam)
	}

	got, err = gh.FindPR(context.Background(), "present")
	if err != nil || got == nil || got.Number != 42 {
		t.Fatalf("expected PR #42, got %+v err=%v", got, err)
	}
}

func TestCreatePR_SendsCorrectPayload(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"number":7,"base":{"ref":"feat-a"},"head":{"ref":"feat-b"}}`)
	}))
	defer srv.Close()
	gh := newTestGH(srv)

	pr, err := gh.CreatePR(context.Background(), NewPR{Title: "T", Head: "feat-b", Base: "feat-a"})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 7 {
		t.Fatalf("expected #7, got %d", pr.Number)
	}
	if body["head"] != "feat-b" || body["base"] != "feat-a" || body["title"] != "T" {
		t.Fatalf("bad payload: %+v", body)
	}
}

func TestPublishStack_PreservesBodyAndEmbeds(t *testing.T) {
	var patched map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, `{"number":3,"body":"Human description."}`)
		case http.MethodPatch:
			json.NewDecoder(r.Body).Decode(&patched)
			io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	gh := newTestGH(srv)

	s := stack.New("main")
	s.Add("feat", "main", "deadbeef")

	if err := gh.PublishStack(context.Background(), 3, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patched["body"], "Human description.") {
		t.Fatalf("human body lost: %q", patched["body"])
	}
	if !strings.Contains(patched["body"], "<!--") {
		t.Fatalf("state block not embedded: %q", patched["body"])
	}
}
