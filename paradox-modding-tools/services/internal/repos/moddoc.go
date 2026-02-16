package repos

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type DocFile struct {
	RelPath   string `db:"rel_path"`
	FetchedAt string `db:"fetched_at"`
}

type ModDocRepository struct {
	db *sqlx.DB
}

func NewModDocRepository(db *sqlx.DB) *ModDocRepository {
	return &ModDocRepository{db: db}
}

func (r *ModDocRepository) UpsertDocFile(game, hash, relPath, absPath, content, fetchedAt string) error {
	_, err := r.db.Exec(`INSERT INTO doc_files (game, install_path_hash, rel_path, abs_path, content, fetched_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(game, install_path_hash, rel_path) DO UPDATE SET abs_path=excluded.abs_path, content=excluded.content, fetched_at=excluded.fetched_at`,
		game, hash, relPath, absPath, content, fetchedAt)
	return err
}

func (r *ModDocRepository) GetDocFiles(game, hash string) ([]DocFile, error) {
	var files []DocFile
	err := r.db.Select(&files, `SELECT rel_path, fetched_at FROM doc_files WHERE game = ? AND install_path_hash = ? ORDER BY rel_path`, game, hash)
	return files, err
}

func (r *ModDocRepository) GetDocContent(game, hash, relPath string) (string, error) {
	var content string
	err := r.db.Get(&content, `SELECT content FROM doc_files WHERE game = ? AND install_path_hash = ? AND rel_path = ?`, game, hash, relPath)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return content, err
}
