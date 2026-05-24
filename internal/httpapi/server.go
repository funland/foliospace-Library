package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"foliospace-reader/internal/service"
)

type Server struct {
	service *service.Service
	static  http.Handler
}

func New(service *service.Service, static http.Handler) *Server {
	return &Server{service: service, static: static}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/libraries", s.handleLibraries)
	mux.HandleFunc("/api/libraries/", s.handleLibraryAction)
	mux.HandleFunc("/api/series", s.handleSeries)
	mux.HandleFunc("/api/series/", s.handleSeriesAction)
	mux.HandleFunc("/api/books/", s.handleBookAction)
	mux.HandleFunc("/api/jobs", s.handleJobs)
	mux.HandleFunc("/api/jobs/", s.handleJobAction)
	mux.HandleFunc("/api/errors", s.handleErrors)
	mux.HandleFunc("/favicon.ico", s.handleFavicon)
	mux.HandleFunc("/", s.handleStatic)
	return mux
}

func (s *Server) handleLibraries(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.service.ListLibraries()
		writeJSONOrError(w, items, err)
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			RootPath string `json:"rootPath"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		lib, err := s.service.CreateLibrary(req.Name, req.RootPath)
		writeJSONOrError(w, lib, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLibraryAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/libraries/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if tail == "" && r.Method == http.MethodDelete {
		writeJSONOrError(w, map[string]bool{"ok": true}, s.service.DeleteLibrary(id))
		return
	}
	if tail != "scan" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	job, err := s.service.ScanLibrary(id)
	writeJSONOrError(w, job, err)
}

func (s *Server) handleSeries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ListSeries()
	writeJSONOrError(w, items, err)
}

func (s *Server) handleSeriesAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseIDAction(r.URL.Path, "/api/series/")
	if !ok || action != "books" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ListBooks(id)
	writeJSONOrError(w, items, err)
}

func (s *Server) handleBookAction(w http.ResponseWriter, r *http.Request) {
	id, tail, ok := parseIDTail(r.URL.Path, "/api/books/")
	if !ok {
		http.NotFound(w, r)
		return
	}

	if tail == "" && r.Method == http.MethodGet {
		book, err := s.service.Book(id)
		writeJSONOrError(w, book, err)
		return
	}
	if tail == "cover" && r.Method == http.MethodGet {
		s.streamPage(w, id, 0)
		return
	}
	if tail == "pages" && r.Method == http.MethodGet {
		pages, err := s.service.Pages(id)
		writeJSONOrError(w, pages, err)
		return
	}
	if strings.HasPrefix(tail, "pages/") && r.Method == http.MethodGet {
		pageIndex, err := strconv.Atoi(strings.TrimPrefix(tail, "pages/"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.streamPage(w, id, pageIndex)
		return
	}
	if tail == "progress" && r.Method == http.MethodPut {
		var req struct {
			PageIndex int `json:"pageIndex"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSONOrError(w, map[string]bool{"ok": true}, s.service.SaveProgress(id, req.PageIndex))
		return
	}
	if tail == "analyze" && r.Method == http.MethodPost {
		pages, err := s.service.AnalyzeBook(id)
		writeJSONOrError(w, pages, err)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) streamPage(w http.ResponseWriter, bookID int64, pageIndex int) {
	page, err := s.service.OpenPage(bookID, pageIndex)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer page.Body.Close()

	w.Header().Set("Content-Type", page.ContentType)
	_, _ = io.Copy(w, page.Body)
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.ListJobs()
	writeJSONOrError(w, items, err)
}

func (s *Server) handleJobAction(w http.ResponseWriter, r *http.Request) {
	id, action, ok := parseIDAction(r.URL.Path, "/api/jobs/")
	if !ok || action != "events" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	items, err := s.service.JobEvents(id)
	writeJSONOrError(w, items, err)
}

func (s *Server) handleErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var jobID int64
	if value := r.URL.Query().Get("jobId"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		jobID = parsed
	}
	var items any
	var err error
	if jobID > 0 {
		items, err = s.service.ListErrorsByJob(jobID)
	} else {
		items, err = s.service.ListErrors()
	}
	writeJSONOrError(w, items, err)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static != nil {
		s.static.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("FolioSpace Reader"))
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func parseIDAction(path string, prefix string) (int64, string, bool) {
	id, tail, ok := parseIDTail(path, prefix)
	if !ok || tail == "" || strings.Contains(tail, "/") {
		return 0, "", false
	}
	return id, tail, true
}

func parseIDTail(path string, prefix string) (int64, string, bool) {
	rest := strings.TrimPrefix(path, prefix)
	if rest == path || rest == "" {
		return 0, "", false
	}
	parts := strings.SplitN(rest, "/", 2)
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	tail := ""
	if len(parts) == 2 {
		tail = parts[1]
	}
	return id, tail, true
}

func writeJSONOrError(w http.ResponseWriter, value any, err error) {
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, value)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
