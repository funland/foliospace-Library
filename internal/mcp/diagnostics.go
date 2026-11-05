package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"foliospace-reader/internal/domain"
)

type diagnosticSpec struct {
	id, path, description string
}

var diagnosticSpecs = map[string]diagnosticSpec{
	"foliospace.diagnose_book": {"bookId", "/api/client/diagnostics/books/", "Read stored book health and page-index diagnostics. Does not open or verify source files, analyze, scan, or modify state. An unknown health status is not a damage report. Optional profileId must exist; no private reader state is returned."},
	"foliospace.diagnose_game": {"gameId", "/api/client/diagnostics/games/", "Read stored game file, checksum, catalog and launch-profile diagnostics. Does not resolve a live launch, access source files or providers, scan, analyze, or modify state. Ready refers only to stored launch profiles. Optional profileId must exist; no private play state is returned."},
}

func diagnosticTools() []Tool {
	out := make([]Tool, 0, len(diagnosticSpecs))
	for _, name := range []string{"foliospace.diagnose_book", "foliospace.diagnose_game"} {
		spec := diagnosticSpecs[name]
		props := map[string]any{}
		for _, key := range []string{spec.id, "profileId"} {
			props[key] = map[string]any{"type": "integer", "minimum": 1, "maximum": 9007199254740991}
		}
		schema := objectSchema(props, []string{spec.id})
		schema["additionalProperties"] = false
		out = append(out, Tool{Name: name, Description: spec.description, InputSchema: schema, Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false}})
	}
	return out
}

func (s *Server) callDiagnosticTool(ctx context.Context, spec diagnosticSpec, args map[string]any) map[string]any {
	var id, profileID int64
	for key, value := range args {
		if key != spec.id && key != "profileId" {
			return videoError("invalid-arguments", 0, nil)
		}
		n, ok := value.(float64)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > 9007199254740991 || math.Trunc(n) != n {
			return videoError("invalid-arguments", 0, nil)
		}
		if key == spec.id {
			id = int64(n)
		} else {
			profileID = int64(n)
		}
	}
	if id == 0 {
		return videoError("invalid-arguments", 0, nil)
	}
	info, failure := s.videoGet(ctx, "/api/client/info")
	if failure != nil {
		return diagnosticFailure(failure)
	}
	object, ok := info.(map[string]any)
	if !ok {
		return videoError("invalid-response", 0, nil)
	}
	caps, _ := object["capabilities"].(map[string]any)
	if caps["storedDiagnosticsV1"] != true {
		return videoError("capability-unsupported", 0, nil)
	}
	path := spec.path + strconv.FormatInt(id, 10)
	if profileID > 0 {
		path += "?profileId=" + strconv.FormatInt(profileID, 10)
	}
	data, failure := s.videoGet(ctx, path)
	if failure != nil {
		return diagnosticFailure(failure)
	}
	// Decode only into the explicit diagnostic contract, rather than returning
	// arbitrary upstream fields. Fixed status/code vocabularies also prevent raw
	// diagnostic messages from crossing the MCP boundary in allowed string fields.
	raw, err := json.Marshal(data)
	if err != nil {
		return videoError("invalid-response", 0, nil)
	}
	if spec.id == "bookId" {
		var result domain.BookDiagnostic
		if json.Unmarshal(raw, &result) != nil || result.BookID != id || !diagnosticContextValid(result.ProfileID, profileID, result.Source, result.FilesystemVerified) ||
			!diagnosticOneOf(result.Health.Status, "unknown", "damaged") || !diagnosticOneOf(result.Index.Status, "unknown", "indexed", "incomplete") || result.Index.DeclaredPages < 0 || result.Index.IndexedPages < 0 {
			return videoError("invalid-response", 0, nil)
		}
		if !diagnosticOneOf(result.Health.Code, "", "stored-file-issue", "empty_file", "zero_filled_file", "invalid_book_container", "invalid_epub", "archive_open_failed", "archive_empty") {
			result.Health.Code = "stored-file-issue"
		}
		result.Issues = diagnosticIssues(result.Issues, "stored-file-issue", "analysis-not-recorded", "page-index-empty", "page-count-mismatch")
		return toolResult(result)
	}
	var result domain.GameDiagnostic
	if json.Unmarshal(raw, &result) != nil || result.GameID != id || !diagnosticContextValid(result.ProfileID, profileID, result.Source, result.FilesystemVerified) ||
		!diagnosticOneOf(result.CatalogRole, "unknown", "game", "dependency", "needs-curation") || !diagnosticOneOf(result.Launch.Status, "unknown", "ready", "unavailable") ||
		!diagnosticOneOf(result.MetadataStatus, "unknown", "matched", "unmatched") || !diagnosticOneOf(result.ArtworkStatus, "unknown", "ready", "missing") ||
		result.Files.Count < 0 || result.Files.Checksummed < 0 || result.Files.ChecksumPending < 0 || result.Files.Checksummed > result.Files.Count || result.Files.ChecksumPending != result.Files.Count-result.Files.Checksummed || result.Launch.ReadyProfiles < 0 {
		return videoError("invalid-response", 0, nil)
	}
	result.Issues = diagnosticIssues(result.Issues, "launch-profile-not-recorded", "dependency-only", "curation-required", "file-index-empty", "checksums-incomplete")
	return toolResult(result)
}

func diagnosticContextValid(actual, requested int64, source string, verified bool) bool {
	return actual > 0 && actual <= 9007199254740991 && (requested == 0 || actual == requested) && source == "stored" && !verified
}

func diagnosticOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func diagnosticIssues(issues []string, allowed ...string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, issue := range issues {
		if diagnosticOneOf(issue, allowed...) && !seen[issue] {
			out = append(out, issue)
			seen[issue] = true
		}
	}
	return out
}

// videoGet bounds responses and blocks redirects; discard its optional upstream
// state and unrecognized error codes for the stricter diagnostics contract.
func diagnosticFailure(failure map[string]any) map[string]any {
	var result struct {
		Code   string `json:"code"`
		Status int    `json:"status"`
	}
	if content, ok := failure["content"].([]map[string]string); ok && len(content) == 1 {
		_ = json.Unmarshal([]byte(content[0]["text"]), &result)
	}
	if !diagnosticOneOf(result.Code, "request-failed", "response-read-failed", "response-too-large", "invalid-response", "invalid-arguments", "profile-not-found", "asset-not-found", "diagnostic-unavailable", "method-not-allowed") {
		result.Code = "request-failed"
		if result.Status > 0 {
			result.Code = fmt.Sprintf("http-%d", result.Status)
		}
	}
	return videoError(result.Code, result.Status, nil)
}
