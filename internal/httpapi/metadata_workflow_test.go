package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"foliospace-reader/internal/db"
	"foliospace-reader/internal/domain"
	"foliospace-reader/internal/service"
	"foliospace-reader/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetadataWorkflowHTTPInternalFailureIsSanitized(t *testing.T) {
	w := httptest.NewRecorder()
	metadataWorkflowHTTPResult(w, nil, errors.New("database /private/library failed with bearer-secret"))
	if w.Code != 500 || !strings.Contains(w.Body.String(), "metadata-unavailable") || strings.Contains(w.Body.String(), "/private/") || strings.Contains(w.Body.String(), "bearer-secret") {
		t.Fatalf("internal failure %d %s", w.Code, w.Body.String())
	}
}

func metadataWorkflowHTTPFixture(t *testing.T) (*Server, *store.Store, int64, int64) {
	t.Helper()
	t.Setenv("FOLIOSPACE_VIDEO_METADATA_PROVIDER", "")
	t.Setenv("FOLIOSPACE_TMDB_TOKEN", "")
	c, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	st := store.New(c)
	lib, err := st.CreateLibraryWithType("Synthetic Movies", t.TempDir(), "video")
	if err != nil {
		t.Fatal(err)
	}
	v, err := st.UpsertVideo(domain.VideoAsset{LibraryID: lib.ID, FilePath: lib.RootPath + "/private-synthetic.mkv", Title: "Original", Format: "mkv", MTime: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	svc := service.New(st)
	t.Cleanup(svc.CloseVideoMetadataWorker)
	job, err := st.QueueVideoMetadata(v.ID, "Synthetic Movie", "movie")
	if err != nil {
		t.Fatal(err)
	}
	claimed, found, err := st.ClaimVideoMetadataJob()
	if err != nil || !found {
		t.Fatalf("claim %+v %v", claimed, err)
	}
	if err = st.FinishVideoMetadataJob(claimed, []domain.VideoCandidate{{ID: "42", Title: "Synthetic Movie", Kind: "movie", Year: 2025, Overview: "Synthetic overview"}}, "", 0); err != nil {
		t.Fatal(err)
	}
	return NewWithOptions(svc, nil, Options{APIToken: "synthetic-admin"}), st, v.ID, job.ID
}

func metadataWorkflowHTTPRequest(s *Server, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	// Call directly to verify this handler authenticates independently of Routes.
	s.handleVideoMetadataWorkflow(w, req)
	return w
}

func TestMetadataWorkflowHTTPPreviewConfirmAndReplay(t *testing.T) {
	s, st, videoID, jobID := metadataWorkflowHTTPFixture(t)
	base := fmt.Sprintf("/api/client/metadata/videos/%d/", videoID)
	body := fmt.Sprintf(`{"profileId":1,"jobId":%d,"candidateIndex":0}`, jobID)
	w := metadataWorkflowHTTPRequest(s, "POST", base+"preview", body, "synthetic-admin")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var preview domain.VideoMetadataPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Scope != "shared-library" || preview.Receipt == "" || preview.VideoID != videoID || preview.Proposed.ProviderID != "42" || preview.ExpiresAt.Before(time.Now()) {
		t.Fatalf("%+v", preview)
	}
	if strings.Contains(w.Body.String(), "private-synthetic") || strings.Contains(w.Body.String(), "filePath") {
		t.Fatal("private path leaked")
	}
	before, err := st.VideoMetadata(videoID)
	if err != nil || before.WorkID != 0 {
		t.Fatal("preview applied a match", before, err)
	}
	apply := fmt.Sprintf(`{"profileId":1,"receipt":%q}`, preview.Receipt)
	w = metadataWorkflowHTTPRequest(s, "POST", base+"apply", apply, "synthetic-admin")
	if w.Code != 400 {
		t.Fatalf("confirmation bypass %d %s", w.Code, w.Body.String())
	}
	apply = fmt.Sprintf(`{"profileId":1,"receipt":%q,"confirm":true}`, preview.Receipt)
	w = metadataWorkflowHTTPRequest(s, "POST", base+"apply", apply, "synthetic-admin")
	if w.Code != 200 {
		t.Fatalf("apply %d %s", w.Code, w.Body.String())
	}
	after, err := st.VideoMetadata(videoID)
	if err != nil || after.WorkID == 0 {
		t.Fatal("match missing", after, err)
	}
	w = metadataWorkflowHTTPRequest(s, "POST", base+"apply", apply, "synthetic-admin")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "metadata-receipt-invalid") {
		t.Fatalf("replay %d %s", w.Code, w.Body.String())
	}
}

func TestMetadataWorkflowHTTPRejectsUnauthorizedAndMalformed(t *testing.T) {
	s, _, videoID, jobID := metadataWorkflowHTTPFixture(t)
	base := fmt.Sprintf("/api/client/metadata/videos/%d/", videoID)
	for _, tc := range []struct {
		method, path, body, token string
		status                    int
	}{
		{"POST", base + "preview", `{}`, "", 401},
		{"POST", base + "apply", `{"profileId":1,"receipt":"x","confirm":true}`, "wrong", 401},
		{"POST", base + "search", `{"profileId":1,"query":"Synthetic","kind":"movie"}`, "synthetic-admin", 503},
		{"POST", base + "search", `{"profileId":999999,"query":"Synthetic","kind":"movie"}`, "synthetic-admin", 404},
		{"POST", base + "search", `{"profileId":0,"query":"Synthetic","kind":"movie"}`, "synthetic-admin", 400},
		{"POST", base + "search", `{"profileId":1,"query":"Synthetic","kind":"poster"}`, "synthetic-admin", 400},
		{"POST", base + "search", `{"profileId":1,"query":"Synthetic","kind":"movie","autoApply":true}`, "synthetic-admin", 400},
		{"POST", base + "preview", fmt.Sprintf(`{"profileId":1,"jobId":%d}`, jobID), "synthetic-admin", 400},
		{"POST", base + "preview", fmt.Sprintf(`{"profileId":1,"jobId":%d,"candidateIndex":0,"title":"Client metadata"}`, jobID), "synthetic-admin", 400},
		{"POST", base + "preview", fmt.Sprintf(`{"profileId":1,"jobId":%d,"candidateIndex":0,"season":0,"episode":0}`, jobID), "synthetic-admin", 400},
		{"POST", base + "preview", fmt.Sprintf(`{"profileId":1,"jobId":%d,"candidateIndex":0} {}`, jobID), "synthetic-admin", 400},
		{"POST", base + "preview", `null`, "synthetic-admin", 400},
		{"POST", base + "preview", strings.Repeat(" ", 8193) + `{}`, "synthetic-admin", 400},
		{"POST", base + "preview?profileId=1", `{}`, "synthetic-admin", 400},
		{"POST", base + "apply", `{"profileId":1,"receipt":"x","confirm":"true"}`, "synthetic-admin", 400},
		{"POST", base + "apply", `{"profileId":1,"receipt":"x","confirm":false}`, "synthetic-admin", 400},
		{"POST", base + "apply", `{"profileId":1,"receipt":"x","confirm":true,"candidate":{"id":"42"}}`, "synthetic-admin", 400},
		{"GET", base + "apply", "", "synthetic-admin", 405},
		{"GET", fmt.Sprintf("%sjobs/%d?profileId=1", base, jobID), "", "synthetic-admin", 200},
		{"GET", fmt.Sprintf("%sjobs/%d?profileId=999999", base, jobID), "", "synthetic-admin", 404},
		{"GET", fmt.Sprintf("%sjobs/%d", base, jobID), "", "synthetic-admin", 400},
		{"GET", fmt.Sprintf("%sjobs/%d?profileId=1&profileId=2", base, jobID), "", "synthetic-admin", 400},
		{"GET", fmt.Sprintf("%sjobs/%d?profileId=1&unexpected=x", base, jobID), "", "synthetic-admin", 400},
	} {
		w := metadataWorkflowHTTPRequest(s, tc.method, tc.path, tc.body, tc.token)
		if w.Code != tc.status {
			t.Fatalf("%s %s %s: %d %s", tc.method, tc.path, tc.body, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-synthetic") || strings.Contains(w.Body.String(), "synthetic-admin") {
			t.Fatal("sensitive data leaked")
		}
	}
	s.options.APIToken = ""
	w := metadataWorkflowHTTPRequest(s, "POST", base+"apply", `{"profileId":1,"receipt":"x","confirm":true}`, "synthetic-admin")
	if w.Code != 401 {
		t.Fatalf("unconfigured auth was bypassed: %d", w.Code)
	}
}

func TestMetadataWorkflowHTTPReceiptScopeAndFreshness(t *testing.T) {
	s, st, videoID, jobID := metadataWorkflowHTTPFixture(t)
	profile, err := st.CreateProfile("Second synthetic profile", "", "")
	if err != nil {
		t.Fatal(err)
	}
	base := fmt.Sprintf("/api/client/metadata/videos/%d/", videoID)
	w := metadataWorkflowHTTPRequest(s, "POST", base+"preview", fmt.Sprintf(`{"profileId":1,"jobId":%d,"candidateIndex":0}`, jobID), "synthetic-admin")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var preview domain.VideoMetadataPreview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	w = metadataWorkflowHTTPRequest(s, "POST", base+"apply", fmt.Sprintf(`{"profileId":%d,"receipt":%q,"confirm":true}`, profile.ID, preview.Receipt), "synthetic-admin")
	if w.Code != 409 {
		t.Fatal("receipt crossed profile", w.Code, w.Body.String())
	}
	w = metadataWorkflowHTTPRequest(s, "POST", fmt.Sprintf("/api/client/metadata/videos/%d/apply", videoID+100), fmt.Sprintf(`{"profileId":1,"receipt":%q,"confirm":true}`, preview.Receipt), "synthetic-admin")
	if w.Code != 409 {
		t.Fatal("receipt crossed target", w.Code, w.Body.String())
	}
	if err := st.SetVideoMetadataOverride(videoID, "overview", `""`); err != nil {
		t.Fatal(err)
	}
	w = metadataWorkflowHTTPRequest(s, "POST", base+"apply", fmt.Sprintf(`{"profileId":1,"receipt":%q,"confirm":true}`, preview.Receipt), "synthetic-admin")
	if w.Code != 409 {
		t.Fatal("stale receipt applied", w.Code, w.Body.String())
	}
	m, err := st.VideoMetadata(videoID)
	if err != nil || m.WorkID != 0 {
		t.Fatal("rejected apply changed match", m, err)
	}
}

func TestMetadataWorkflowHTTPRegisteredRouteAndAuth(t *testing.T) {
	s, _, videoID, jobID := metadataWorkflowHTTPFixture(t)
	path := fmt.Sprintf("/api/client/metadata/videos/%d/jobs/%d?profileId=1", videoID, jobID)
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer synthetic-admin")
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic Movie") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
