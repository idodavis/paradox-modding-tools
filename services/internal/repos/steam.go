package repos

import (
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"
)

type PatchNote struct {
	Game       string `db:"game"`
	FetchedAt  string `db:"fetched_at"`
	Title      string `db:"title"`
	Contents   string `db:"contents"`
	SteamURL   string `db:"steam_url"`
	SteamDBURL string `db:"steamdb_url"`
}

type SteamRepository struct {
	db *sqlx.DB
}

func NewSteamRepository(db *sqlx.DB) *SteamRepository {
	return &SteamRepository{db: db}
}

func (r *SteamRepository) GetPatchNote(game string) (*PatchNote, error) {
	var note PatchNote
	err := r.db.Get(&note, `SELECT fetched_at, title, contents, steam_url, steamdb_url FROM patchnotes WHERE game = ?`, game)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &note, err
}

func (r *SteamRepository) UpsertPatchNote(game, title, contents, steamURL, steamDBURL string) error {
	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	_, err := r.db.Exec(`INSERT INTO patchnotes (game, fetched_at, title, contents, steam_url, steamdb_url) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(game) DO UPDATE SET fetched_at=excluded.fetched_at, title=excluded.title, contents=excluded.contents, steam_url=excluded.steam_url, steamdb_url=excluded.steamdb_url`,
		game, fetchedAt, title, contents, steamURL, steamDBURL)
	return err
}
