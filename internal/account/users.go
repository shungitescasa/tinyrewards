package account

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
)

const PlatformTwitch = "twitch"

const mysqlDuplicateEntry = 1062

var ErrUsernameTaken = errors.New("username is already taken")

type User struct {
	ID       string    `json:"id"`
	Username string    `json:"username"`
	JoinedAt time.Time `json:"joined_at"`
}

func (h *Handler) login(ctx context.Context, platform, aliasID, username string) (User, error) {
	u, err := h.findByConnection(ctx, platform, aliasID)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return User{}, err
	}

	u, err = h.register(ctx, platform, aliasID, username)
	if !isDuplicate(err) {
		return u, err
	}

	u, err = h.findByConnection(ctx, platform, aliasID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUsernameTaken
	}
	return u, err
}

func (h *Handler) findByConnection(ctx context.Context, platform, aliasID string) (User, error) {
	var u User
	err := h.db.QueryRowContext(ctx,
		`SELECT u.id, u.username, u.joined_at
		 FROM connections c
		 JOIN users u ON u.id = c.user_id
		 WHERE c.platform = ? AND c.alias_id = ?`,
		platform, aliasID,
	).Scan(&u.ID, &u.Username, &u.JoinedAt)
	return u, err
}

func (h *Handler) FindByID(ctx context.Context, id string) (User, error) {
	var u User
	err := h.db.QueryRowContext(ctx,
		`SELECT id, username, joined_at FROM users WHERE id = ?`,
		id,
	).Scan(&u.ID, &u.Username, &u.JoinedAt)
	return u, err
}

type Connection struct {
	Platform    string    `json:"platform"`
	AliasID     string    `json:"alias_id"`
	ConnectedAt time.Time `json:"connected_at"`
}

func (h *Handler) Connections(ctx context.Context, userID string) ([]Connection, error) {
	rows, err := h.db.QueryContext(ctx,
		`SELECT platform, alias_id, connected_at FROM connections WHERE user_id = ? ORDER BY connected_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Connection
	for rows.Next() {
		var conn Connection
		if err := rows.Scan(&conn.Platform, &conn.AliasID, &conn.ConnectedAt); err != nil {
			return nil, err
		}
		list = append(list, conn)
	}
	return list, rows.Err()
}

func (h *Handler) register(ctx context.Context, platform, aliasID, username string) (User, error) {
	id := make([]byte, 16)
	rand.Read(id)
	u := User{ID: hex.EncodeToString(id), Username: username}

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO users (id, username) VALUES (?, ?)`, u.ID, u.Username); err != nil {
		return User{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO connections (user_id, platform, alias_id) VALUES (?, ?, ?)`,
		u.ID, platform, aliasID,
	); err != nil {
		return User{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT joined_at FROM users WHERE id = ?`, u.ID).Scan(&u.JoinedAt); err != nil {
		return User{}, err
	}
	return u, tx.Commit()
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == mysqlDuplicateEntry
}
