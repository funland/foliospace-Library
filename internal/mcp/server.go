package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
}

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

const serviceVersion = "0.881"

func New(baseURL string, token string) *Server {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	return &Server{
		baseURL: baseURL,
		token:   strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (s *Server) Handle(ctx context.Context, req Request) Response {
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}
	switch req.Method {
	case "initialize":
		return ok(req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "foliospace-library",
				"version": serviceVersion,
			},
		})
	case "tools/list":
		return ok(req.ID, map[string]any{"tools": tools()})
	case "resources/list":
		return ok(req.ID, map[string]any{"resources": resources()})
	case "resources/read":
		result, err := s.readResource(ctx, req.Params)
		if err != nil {
			return fail(req.ID, -32000, err.Error())
		}
		return ok(req.ID, result)
	case "tools/call":
		result, err := s.callTool(ctx, req.Params)
		if err != nil {
			return fail(req.ID, -32000, err.Error())
		}
		return ok(req.ID, result)
	default:
		if strings.HasPrefix(req.Method, "notifications/") {
			return Response{}
		}
		return fail(req.ID, -32601, "method not found: "+req.Method)
	}
}

func tools() []Tool {
	return []Tool{
		{Name: "foliospace.client_info", Description: "Return FolioSpace service name, version, supported formats, and capability flags.", InputSchema: objectSchema(nil, nil)},
		{Name: "foliospace.home", Description: "Return the agent-friendly home payload: continue reading, recent books, and collections.", InputSchema: objectSchema(map[string]any{"limit": integerSchema("Maximum number of items per section.")}, nil)},
		{Name: "foliospace.search_books", Description: "Search indexed books and comics by title, author, or collection context.", InputSchema: objectSchema(map[string]any{"q": stringSchema("Search query."), "limit": integerSchema("Maximum number of results.")}, []string{"q"})},
		{Name: "foliospace.open_book_manifest", Description: "Open a book/comic manifest with client-safe page, EPUB, progress, and state URLs.", InputSchema: objectSchema(map[string]any{"bookId": integerSchema("Book id.")}, []string{"bookId"})},
		{Name: "foliospace.list_games", Description: "List paginated client-safe game ROM assets.", InputSchema: objectSchema(map[string]any{"limit": integerSchema("Maximum number of items."), "offset": integerSchema("Zero-based item offset."), "q": stringSchema("Optional search query."), "platform": stringSchema("Optional exact platform filter."), "format": stringSchema("Optional exact format filter."), "sort": stringSchema("recent, title, or platform.")}, nil)},
		{Name: "foliospace.open_game_manifest", Description: "Open a game ROM manifest with metadata, cover URL, and opaque file URL.", InputSchema: objectSchema(map[string]any{"gameId": integerSchema("Game asset id.")}, []string{"gameId"})},
		{Name: "foliospace.list_videos", Description: "List paginated client-safe video assets.", InputSchema: objectSchema(map[string]any{"limit": integerSchema("Maximum number of items."), "offset": integerSchema("Zero-based item offset."), "q": stringSchema("Optional search query."), "format": stringSchema("Optional exact format filter."), "sort": stringSchema("recent or title.")}, nil)},
		{Name: "foliospace.open_video_manifest", Description: "Open a video manifest with metadata, thumbnail URL, and opaque range-stream file URL.", InputSchema: objectSchema(map[string]any{"videoId": integerSchema("Video asset id.")}, []string{"videoId"})},
		{Name: "foliospace.get_video_transcode_status", Description: "Read HLS transcode state for a video asset, including queued, running, ready, or failed.", InputSchema: objectSchema(map[string]any{"videoId": integerSchema("Video asset id.")}, []string{"videoId"})},
		{Name: "foliospace.get_video_transcode_queue", Description: "Read the active global video transcode task, if any.", InputSchema: objectSchema(nil, nil)},
		{Name: "foliospace.get_preferences", Description: "Read client preferences such as interface language, reader settings, and feature defaults.", InputSchema: objectSchema(nil, nil)},
		{Name: "foliospace.save_preferences", Description: "Save client preferences. Pass the same JSON shape as the HTTP Client API.", InputSchema: objectSchema(map[string]any{"interfaceLanguage": stringSchema("Interface language code, for example zh-Hans, zh-Hant, en, or ja.")}, nil)},
		{Name: "foliospace.get_scan_settings", Description: "Read scan runtime settings such as worker count.", InputSchema: objectSchema(nil, nil)},
		{Name: "foliospace.save_scan_settings", Description: "Save scan runtime settings. scanWorkers is normalized by the server.", InputSchema: objectSchema(map[string]any{"scanWorkers": integerSchema("Concurrent scan worker count, normalized by the server.")}, []string{"scanWorkers"})},
		{Name: "foliospace.get_private_state", Description: "Read per-book private reader state such as bookmarks, notes, selected text, or local-only UI state.", InputSchema: objectSchema(map[string]any{"bookId": integerSchema("Book id.")}, []string{"bookId"})},
		{Name: "foliospace.save_private_state", Description: "Save per-book private reader state. bookId selects the book; remaining fields are forwarded to the API.", InputSchema: objectSchema(map[string]any{"bookId": integerSchema("Book id.")}, []string{"bookId"})},
		{Name: "foliospace.list_favorites", Description: "List favorite books as client-safe DTOs.", InputSchema: objectSchema(map[string]any{"limit": integerSchema("Maximum number of results.")}, nil)},
		{Name: "foliospace.list_private_status", Description: "List books with a private status such as want, reading, finished, or dropped.", InputSchema: objectSchema(map[string]any{"status": stringSchema("Private status."), "limit": integerSchema("Maximum number of results.")}, []string{"status"})},
		{Name: "foliospace.get_progress", Description: "Read saved reading progress for a book.", InputSchema: objectSchema(map[string]any{"bookId": integerSchema("Book id.")}, []string{"bookId"})},
		{Name: "foliospace.save_progress", Description: "Save reading progress for a book. bookId selects the book; remaining fields are forwarded to the API.", InputSchema: objectSchema(map[string]any{"bookId": integerSchema("Book id.")}, []string{"bookId"})},
		{Name: "foliospace.list_libraries", Description: "List configured library roots for diagnostics and scan selection. This admin tool can expose configured mount paths.", InputSchema: objectSchema(nil, nil)},
		{Name: "foliospace.list_collections", Description: "List library collections.", InputSchema: objectSchema(nil, nil)},
		{Name: "foliospace.list_collection_volumes", Description: "List books/comics in a collection with optional pagination and filtering.", InputSchema: objectSchema(map[string]any{"collectionId": integerSchema("Collection id."), "limit": integerSchema("Maximum number of items."), "offset": integerSchema("Zero-based item offset."), "q": stringSchema("Optional search query."), "sort": stringSchema("Server-supported sort key.")}, []string{"collectionId"})},
		{Name: "foliospace.list_collection_assets", Description: "List mixed assets in a collection, including books, comics, games, documents, and media as available.", InputSchema: objectSchema(map[string]any{"collectionId": integerSchema("Collection id.")}, []string{"collectionId"})},
		{Name: "foliospace.scan_library", Description: "Start a scan for a configured library.", InputSchema: objectSchema(map[string]any{"libraryId": integerSchema("Library id.")}, []string{"libraryId"})},
		{Name: "foliospace.list_jobs", Description: "List scan/import jobs.", InputSchema: objectSchema(nil, nil)},
		{Name: "foliospace.job_events", Description: "List events for a scan/import job.", InputSchema: objectSchema(map[string]any{"jobId": integerSchema("Job id.")}, []string{"jobId"})},
		{Name: "foliospace.pause_job", Description: "Request pause for a running scan job.", InputSchema: objectSchema(map[string]any{"jobId": integerSchema("Job id.")}, []string{"jobId"})},
		{Name: "foliospace.cancel_job", Description: "Request cancellation for a running, pause-requested, or paused scan job.", InputSchema: objectSchema(map[string]any{"jobId": integerSchema("Job id.")}, []string{"jobId"})},
		{Name: "foliospace.resume_job", Description: "Resume a paused scan job by starting a new scan for the same library.", InputSchema: objectSchema(map[string]any{"jobId": integerSchema("Paused job id.")}, []string{"jobId"})},
		{Name: "foliospace.list_errors", Description: "List scan/import errors, optionally filtered by job id.", InputSchema: objectSchema(map[string]any{"jobId": integerSchema("Optional job id filter.")}, nil)},
		{Name: "foliospace.library_health", Description: "Return service info plus current job and error counts for agent diagnostics.", InputSchema: objectSchema(nil, nil)},
	}
}

func resources() []Resource {
	return []Resource{
		{URI: "foliospace://client/info", Name: "Client Info", Description: "Current FolioSpace Library service metadata.", MimeType: "application/json"},
		{URI: "foliospace://client/home", Name: "Home", Description: "Continue reading, recent books, and collections.", MimeType: "application/json"},
		{URI: "foliospace://client/videos", Name: "Videos", Description: "Recent client-safe video catalog items.", MimeType: "application/json"},
		{URI: "foliospace://client/preferences", Name: "Preferences", Description: "Client preference state.", MimeType: "application/json"},
		{URI: "foliospace://settings/scan", Name: "Scan Settings", Description: "Scan worker and runtime settings.", MimeType: "application/json"},
		{URI: "foliospace://libraries", Name: "Libraries", Description: "Configured libraries for diagnostics and scan selection.", MimeType: "application/json"},
		{URI: "foliospace://jobs", Name: "Jobs", Description: "Scan/import job list.", MimeType: "application/json"},
		{URI: "foliospace://errors", Name: "Errors", Description: "Scan/import error list.", MimeType: "application/json"},
		{URI: "foliospace://health", Name: "Library Health", Description: "Service, job, and error summary.", MimeType: "application/json"},
	}
}

func (s *Server) readResource(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("decode resource params: %w", err)
	}
	var data any
	var err error
	switch params.URI {
	case "foliospace://client/info":
		data, err = s.get(ctx, "/api/client/info")
	case "foliospace://client/home":
		data, err = s.get(ctx, "/api/client/home")
	case "foliospace://client/videos":
		data, err = s.get(ctx, "/api/client/videos?limit=20")
	case "foliospace://client/preferences":
		data, err = s.get(ctx, "/api/client/preferences")
	case "foliospace://settings/scan":
		data, err = s.get(ctx, "/api/settings/scan")
	case "foliospace://libraries":
		data, err = s.get(ctx, "/api/libraries")
	case "foliospace://jobs":
		data, err = s.get(ctx, "/api/jobs")
	case "foliospace://errors":
		data, err = s.get(ctx, "/api/errors")
	case "foliospace://health":
		data, err = s.libraryHealth(ctx)
	default:
		return nil, fmt.Errorf("unknown resource: %s", params.URI)
	}
	if err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"contents": []map[string]string{
			{"uri": params.URI, "mimeType": "application/json", "text": string(encoded)},
		},
	}, nil
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("decode tool params: %w", err)
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	var data any
	var err error
	switch params.Name {
	case "foliospace.client_info":
		data, err = s.get(ctx, "/api/client/info")
	case "foliospace.home":
		data, err = s.get(ctx, "/api/client/home?"+limitQuery(params.Arguments, 12))
	case "foliospace.search_books":
		query := stringArg(params.Arguments, "q")
		if query == "" {
			query = stringArg(params.Arguments, "query")
		}
		data, err = s.get(ctx, "/api/client/search?q="+url.QueryEscape(query)+"&"+limitQuery(params.Arguments, 20))
	case "foliospace.open_book_manifest":
		data, err = s.get(ctx, fmt.Sprintf("/api/client/books/%d/manifest", intArg(params.Arguments, "bookId")))
	case "foliospace.list_games":
		data, err = s.get(ctx, "/api/client/games?"+gameListQuery(params.Arguments))
	case "foliospace.open_game_manifest":
		data, err = s.get(ctx, fmt.Sprintf("/api/client/games/%d/manifest", intArg(params.Arguments, "gameId")))
	case "foliospace.list_videos":
		data, err = s.get(ctx, "/api/client/videos?"+videoListQuery(params.Arguments))
	case "foliospace.open_video_manifest":
		data, err = s.get(ctx, fmt.Sprintf("/api/client/videos/%d/manifest", intArg(params.Arguments, "videoId")))
	case "foliospace.get_video_transcode_status":
		data, err = s.get(ctx, fmt.Sprintf("/api/client/videos/%d/transcode/status", intArg(params.Arguments, "videoId")))
	case "foliospace.get_video_transcode_queue":
		data, err = s.get(ctx, "/api/client/videos/transcode/status")
	case "foliospace.get_preferences":
		data, err = s.get(ctx, "/api/client/preferences")
	case "foliospace.save_preferences":
		data, err = s.put(ctx, "/api/client/preferences", params.Arguments)
	case "foliospace.get_scan_settings":
		data, err = s.get(ctx, "/api/settings/scan")
	case "foliospace.save_scan_settings":
		data, err = s.put(ctx, "/api/settings/scan", params.Arguments)
	case "foliospace.get_private_state":
		data, err = s.get(ctx, fmt.Sprintf("/api/client/books/%d/private-state", intArg(params.Arguments, "bookId")))
	case "foliospace.save_private_state":
		bookID := intArg(params.Arguments, "bookId")
		body := withoutKeys(params.Arguments, "bookId")
		data, err = s.put(ctx, fmt.Sprintf("/api/client/books/%d/private-state", bookID), body)
	case "foliospace.list_favorites":
		data, err = s.get(ctx, "/api/client/books/favorites?"+limitQuery(params.Arguments, 12))
	case "foliospace.list_private_status":
		status := stringArg(params.Arguments, "status")
		data, err = s.get(ctx, "/api/client/books/private-status/"+url.PathEscape(status)+"?"+limitQuery(params.Arguments, 12))
	case "foliospace.get_progress":
		data, err = s.get(ctx, fmt.Sprintf("/api/books/%d/progress", intArg(params.Arguments, "bookId")))
	case "foliospace.save_progress":
		bookID := intArg(params.Arguments, "bookId")
		body := withoutKeys(params.Arguments, "bookId")
		data, err = s.put(ctx, fmt.Sprintf("/api/books/%d/progress", bookID), body)
	case "foliospace.list_libraries":
		data, err = s.get(ctx, "/api/libraries")
	case "foliospace.list_collections":
		data, err = s.get(ctx, "/api/collections")
	case "foliospace.list_collection_volumes":
		data, err = s.get(ctx, fmt.Sprintf("/api/collections/%d/volumes?%s", intArg(params.Arguments, "collectionId"), collectionVolumesQuery(params.Arguments)))
	case "foliospace.list_collection_assets":
		data, err = s.get(ctx, fmt.Sprintf("/api/collections/%d/assets", intArg(params.Arguments, "collectionId")))
	case "foliospace.scan_library":
		data, err = s.post(ctx, fmt.Sprintf("/api/libraries/%d/scan", intArg(params.Arguments, "libraryId")), map[string]any{})
	case "foliospace.list_jobs":
		data, err = s.get(ctx, "/api/jobs")
	case "foliospace.job_events":
		data, err = s.get(ctx, fmt.Sprintf("/api/jobs/%d/events", intArg(params.Arguments, "jobId")))
	case "foliospace.pause_job":
		data, err = s.post(ctx, fmt.Sprintf("/api/jobs/%d/pause", intArg(params.Arguments, "jobId")), map[string]any{})
	case "foliospace.cancel_job":
		data, err = s.post(ctx, fmt.Sprintf("/api/jobs/%d/cancel", intArg(params.Arguments, "jobId")), map[string]any{})
	case "foliospace.resume_job":
		data, err = s.post(ctx, fmt.Sprintf("/api/jobs/%d/resume", intArg(params.Arguments, "jobId")), map[string]any{})
	case "foliospace.list_errors":
		if jobID := intArg(params.Arguments, "jobId"); jobID > 0 {
			data, err = s.get(ctx, fmt.Sprintf("/api/errors?jobId=%d", jobID))
		} else {
			data, err = s.get(ctx, "/api/errors")
		}
	case "foliospace.library_health":
		data, err = s.libraryHealth(ctx)
	default:
		return nil, fmt.Errorf("unknown tool: %s", params.Name)
	}
	if err != nil {
		return nil, err
	}
	return toolResult(data), nil
}

func (s *Server) get(ctx context.Context, path string) (any, error) {
	return s.do(ctx, http.MethodGet, path, nil)
}

func (s *Server) post(ctx context.Context, path string, body any) (any, error) {
	return s.do(ctx, http.MethodPost, path, body)
}

func (s *Server) put(ctx context.Context, path string, body any) (any, error) {
	return s.do(ctx, http.MethodPut, path, body)
}

func (s *Server) do(ctx context.Context, method string, path string, body any) (any, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s %s returned %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{"body": string(data)}, nil
	}
	return out, nil
}

func (s *Server) libraryHealth(ctx context.Context) (any, error) {
	info, err := s.get(ctx, "/api/client/info")
	if err != nil {
		return nil, err
	}
	jobs, err := s.get(ctx, "/api/jobs")
	if err != nil {
		return nil, err
	}
	errors, err := s.get(ctx, "/api/errors")
	if err != nil {
		return nil, err
	}
	jobItems, _ := jobs.([]any)
	errorItems, _ := errors.([]any)
	return map[string]any{
		"info":       info,
		"jobCount":   len(jobItems),
		"errorCount": len(errorItems),
		"jobs":       jobs,
		"errors":     errors,
	}, nil
}

func ok(id any, result any) Response {
	return Response{JSONRPC: "2.0", ID: id, Result: result}
}

func fail(id any, code int, message string) Response {
	return Response{JSONRPC: "2.0", ID: id, Error: &ResponseError{Code: code, Message: message}}
}

func toolResult(data any) map[string]any {
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		encoded = []byte(fmt.Sprint(data))
	}
	return map[string]any{
		"content": []map[string]string{
			{"type": "text", "text": string(encoded)},
		},
	}
}

func limitQuery(args map[string]any, fallback int) string {
	limit := int(intArg(args, "limit"))
	if limit <= 0 {
		limit = fallback
	}
	return "limit=" + strconv.Itoa(limit)
}

func gameListQuery(args map[string]any) string {
	values := url.Values{}
	limit := intArg(args, "limit")
	if limit > 0 {
		values.Set("limit", strconv.FormatInt(limit, 10))
	}
	offset := intArg(args, "offset")
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(offset, 10))
	}
	for _, key := range []string{"q", "platform", "format", "sort"} {
		if value := stringArg(args, key); value != "" {
			values.Set(key, value)
		}
	}
	return values.Encode()
}

func videoListQuery(args map[string]any) string {
	values := url.Values{}
	limit := intArg(args, "limit")
	if limit > 0 {
		values.Set("limit", strconv.FormatInt(limit, 10))
	}
	offset := intArg(args, "offset")
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(offset, 10))
	}
	for _, key := range []string{"q", "format", "sort"} {
		if value := stringArg(args, key); value != "" {
			values.Set(key, value)
		}
	}
	return values.Encode()
}

func collectionVolumesQuery(args map[string]any) string {
	values := url.Values{}
	limit := intArg(args, "limit")
	if limit > 0 {
		values.Set("limit", strconv.FormatInt(limit, 10))
	}
	offset := intArg(args, "offset")
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(offset, 10))
	}
	for _, key := range []string{"q", "sort"} {
		if value := stringArg(args, key); value != "" {
			values.Set(key, value)
		}
	}
	return values.Encode()
}

func intArg(args map[string]any, key string) int64 {
	value, ok := args[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func stringArg(args map[string]any, key string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func withoutKeys(args map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(args))
	skip := map[string]bool{}
	for _, key := range keys {
		skip[key] = true
	}
	for key, value := range args {
		if !skip[key] {
			out[key] = value
		}
	}
	return out
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}
