package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrCatalogArguments = errors.New("invalid catalog arguments")
var ErrCatalogProfile = errors.New("catalog profile not found")

// CatalogItem is shared catalog metadata only. It deliberately excludes local
// paths and profile-private tags, notes, favorites and reading/playback state.
type CatalogItem struct {
	AssetType       string `json:"assetType"`
	ID              int64  `json:"id"`
	LibraryID       int64  `json:"libraryId"`
	Title           string `json:"title"`
	Format          string `json:"format"`
	Creator         string `json:"creator,omitempty"`
	CollectionID    int64  `json:"collectionId,omitempty"`
	CollectionTitle string `json:"collectionTitle,omitempty"`
	Platform        string `json:"platform,omitempty"`
	CatalogRole     string `json:"catalogRole,omitempty"`
}

type CatalogOptions struct {
	Query, Format, Sort, Cursor string
	Limit, Offset               int
	ProfileID                   int64
}

type CatalogPage struct {
	Items       []CatalogItem `json:"items"`
	Total       int64         `json:"total"`
	Limit       int           `json:"limit"`
	Offset      int           `json:"offset,omitempty"`
	HasMore     bool          `json:"hasMore"`
	NextCursor  string        `json:"nextCursor,omitempty"`
	ProfileID   int64         `json:"profileId"`
	Scope       string        `json:"scope"`
	Order       string        `json:"order"`
	Consistency string        `json:"consistency"`
}

type catalogCursor struct {
	Version   int      `json:"v"`
	Filter    string   `json:"filter"`
	Max       [3]int64 `json:"max"`
	AfterType int      `json:"type"`
	AfterID   int64    `json:"id"`
}

func normalizeCatalogOptions(o CatalogOptions) (CatalogOptions, error) {
	o.Query = strings.TrimSpace(o.Query)
	o.Format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(o.Format)), ".")
	if o.Format == "all" {
		o.Format = ""
	}
	if o.Limit == 0 {
		o.Limit = 20
	}
	if o.Limit < 1 || o.Limit > 100 || o.Offset < 0 || o.Offset > 10000000 || o.ProfileID < 0 || len(o.Query) > 512 || len(o.Format) > 32 || len(o.Cursor) > 2048 {
		return o, ErrCatalogArguments
	}
	return o, nil
}

type catalogQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func catalogProfile(ctx context.Context, q catalogQueryer, id int64) (int64, error) {
	var resolved int64
	query := "SELECT id FROM profiles WHERE id = ?"
	if id == 0 {
		query = "SELECT id FROM profiles WHERE is_default = 1 ORDER BY id LIMIT 1"
	}
	var err error
	if id == 0 {
		err = q.QueryRowContext(ctx, query).Scan(&resolved)
	} else {
		err = q.QueryRowContext(ctx, query, id).Scan(&resolved)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrCatalogProfile
	}
	return resolved, err
}

func (s *Store) CatalogProfile(ctx context.Context, id int64) (int64, error) {
	if id < 0 {
		return 0, ErrCatalogArguments
	}
	return catalogProfile(ctx, s.db, id)
}

const catalogBookSelect = `SELECT 0 AS rank, 'book' AS asset_type, b.id, s.library_id, b.title, b.format,
 COALESCE(b.creator,''), s.id, s.title, '' AS platform, '' AS catalog_role FROM books b JOIN series s ON s.id=b.series_id`

func catalogBookWhere(o CatalogOptions) (string, []any) {
	parts, args := []string{"1=1"}, []any{}
	if o.Query != "" {
		parts = append(parts, `(LOWER(b.title) LIKE LOWER(?) ESCAPE '\' OR LOWER(s.title) LIKE LOWER(?) ESCAPE '\' OR LOWER(COALESCE(b.creator,'')) LIKE LOWER(?) ESCAPE '\' OR LOWER(COALESCE(b.tags,'')) LIKE LOWER(?) ESCAPE '\')`)
		p := "%" + escapeLike(o.Query) + "%"
		args = append(args, p, p, p, p)
	}
	if o.Format != "" {
		parts = append(parts, "LOWER(b.format)=?")
		args = append(args, o.Format)
	}
	return " WHERE " + strings.Join(parts, " AND "), args
}

func scanCatalogRows(rows *sql.Rows) ([]CatalogItem, []int, error) {
	items, ranks := []CatalogItem{}, []int{}
	for rows.Next() {
		var item CatalogItem
		var rank int
		if err := rows.Scan(&rank, &item.AssetType, &item.ID, &item.LibraryID, &item.Title, &item.Format, &item.Creator, &item.CollectionID, &item.CollectionTitle, &item.Platform, &item.CatalogRole); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		ranks = append(ranks, rank)
	}
	return items, ranks, rows.Err()
}

// CatalogBooks filters in SQL before counting/paging, not after a bounded search.
func (s *Store) CatalogBooks(ctx context.Context, o CatalogOptions) (CatalogPage, error) {
	o, err := normalizeCatalogOptions(o)
	if err != nil || o.Cursor != "" || (o.Sort != "" && o.Sort != "title" && o.Sort != "recent") {
		return CatalogPage{}, ErrCatalogArguments
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CatalogPage{}, err
	}
	defer tx.Rollback()
	profile, err := catalogProfile(ctx, tx, o.ProfileID)
	if err != nil {
		return CatalogPage{}, err
	}
	where, args := catalogBookWhere(o)
	out := CatalogPage{Items: []CatalogItem{}, Limit: o.Limit, Offset: o.Offset, ProfileID: profile, Scope: "shared-catalog", Order: "title,id ascending", Consistency: "offset pagination; restart after catalog changes"}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM books b JOIN series s ON s.id=b.series_id`+where, args...).Scan(&out.Total); err != nil {
		return CatalogPage{}, err
	}
	order := " ORDER BY b.title,b.id"
	if o.Sort == "recent" {
		order = " ORDER BY b.created_at DESC,b.id DESC"
		out.Order = "createdAt,id descending"
	}
	args = append(args, o.Limit, o.Offset)
	rows, err := tx.QueryContext(ctx, catalogBookSelect+where+order+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		return CatalogPage{}, err
	}
	out.Items, _, err = scanCatalogRows(rows)
	rows.Close()
	if err != nil {
		return CatalogPage{}, err
	}
	out.HasMore = int64(o.Offset+len(out.Items)) < out.Total
	return out, tx.Commit()
}

// SearchCatalog pages a SQL UNION by (type rank, primary key). Every physical
// catalog row occurs once; comic books belong to the book namespace, and video
// rows are file versions rather than deduplicated works. The cursor fixes each
// table's maximum ID, so new inserts cannot move page boundaries. Deletions are
// skipped. This is not a metadata snapshot: edits may change query membership.
func (s *Store) SearchCatalog(ctx context.Context, o CatalogOptions) (CatalogPage, error) {
	o, err := normalizeCatalogOptions(o)
	if err != nil || o.Offset != 0 || o.Sort != "" {
		return CatalogPage{}, ErrCatalogArguments
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return CatalogPage{}, err
	}
	defer tx.Rollback()
	profile, err := catalogProfile(ctx, tx, o.ProfileID)
	if err != nil {
		return CatalogPage{}, err
	}
	filterBytes, _ := json.Marshal([]any{o.Query, o.Format, profile})
	hash := sha256.Sum256(filterBytes)
	cur := catalogCursor{Version: 1, Filter: hex.EncodeToString(hash[:]), AfterType: 0}
	if o.Cursor != "" {
		data, e := base64.RawURLEncoding.DecodeString(o.Cursor)
		if e != nil {
			return CatalogPage{}, ErrCatalogArguments
		}
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.DisallowUnknownFields()
		var got catalogCursor
		var extra any
		if dec.Decode(&got) != nil || dec.Decode(&extra) != io.EOF || got.Version != 1 || got.Filter != cur.Filter || got.AfterType < 0 || got.AfterType > 2 || got.AfterID < 0 {
			return CatalogPage{}, ErrCatalogArguments
		}
		for _, n := range got.Max {
			if n < 0 {
				return CatalogPage{}, ErrCatalogArguments
			}
		}
		if got.AfterID > got.Max[got.AfterType] {
			return CatalogPage{}, ErrCatalogArguments
		}
		cur = got
	} else {
		if err = tx.QueryRowContext(ctx, `SELECT (SELECT COALESCE(MAX(id),0) FROM books),(SELECT COALESCE(MAX(id),0) FROM games),(SELECT COALESCE(MAX(id),0) FROM videos)`).Scan(&cur.Max[0], &cur.Max[1], &cur.Max[2]); err != nil {
			return CatalogPage{}, err
		}
	}
	where, args := catalogBookWhere(o)
	query := catalogBookSelect + where + " AND b.id<=?"
	args = append(args, cur.Max[0])
	query += ` UNION ALL SELECT 1,'game',id,library_id,title,format,'',0,'',platform,catalog_role FROM games WHERE id<=?`
	args = append(args, cur.Max[1])
	if o.Query != "" {
		query += ` AND (LOWER(title) LIKE LOWER(?) ESCAPE '\' OR LOWER(platform) LIKE LOWER(?) ESCAPE '\' OR LOWER(rom_set_name) LIKE LOWER(?) ESCAPE '\')`
		p := "%" + escapeLike(o.Query) + "%"
		args = append(args, p, p, p)
	}
	if o.Format != "" {
		query += " AND LOWER(format)=?"
		args = append(args, o.Format)
	}
	query += ` UNION ALL SELECT 2,'video',id,library_id,` + videoDisplayTitleSQL + `,format,'',0,'','','' FROM videos WHERE id<=?`
	args = append(args, cur.Max[2])
	if o.Query != "" {
		query += ` AND (LOWER(` + videoDisplayTitleSQL + `) LIKE LOWER(?) ESCAPE '\' OR LOWER(title) LIKE LOWER(?) ESCAPE '\')`
		p := "%" + escapeLike(o.Query) + "%"
		args = append(args, p, p)
	}
	if o.Format != "" {
		query += " AND LOWER(format)=?"
		args = append(args, o.Format)
	}
	out := CatalogPage{Items: []CatalogItem{}, Limit: o.Limit, ProfileID: profile, Scope: "shared-catalog: books (including comics), all indexed game roles, video files", Order: "book,game,video; id ascending", Consistency: "bounded keyset: later inserts excluded, deletions skipped; metadata edits may change matches, restart to refresh"}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+query+")", args...).Scan(&out.Total); err != nil {
		return CatalogPage{}, err
	}
	pageArgs := append(append([]any{}, args...), cur.AfterType, cur.AfterType, cur.AfterID, o.Limit+1)
	rows, err := tx.QueryContext(ctx, "SELECT * FROM ("+query+") WHERE rank>? OR (rank=? AND id>?) ORDER BY rank,id LIMIT ?", pageArgs...)
	if err != nil {
		return CatalogPage{}, err
	}
	var ranks []int
	out.Items, ranks, err = scanCatalogRows(rows)
	rows.Close()
	if err != nil {
		return CatalogPage{}, err
	}
	out.HasMore = len(out.Items) > o.Limit
	if out.HasMore {
		out.Items = out.Items[:o.Limit]
		cur.AfterType = ranks[o.Limit-1]
		cur.AfterID = out.Items[o.Limit-1].ID
		encoded, e := json.Marshal(cur)
		if e != nil {
			return CatalogPage{}, fmt.Errorf("encode catalog cursor: %w", e)
		}
		out.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return out, tx.Commit()
}
