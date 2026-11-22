package rewards

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/emotes"
	"git.shungites.casa/tiny/rewards/internal/sounds"
)

const (
	TypeChat         = "chat"
	TypeChatEmote    = "chat_emote"
	TypeTwitchReward = "twitch_channel_point_reward"
)

var types = []string{TypeChat, TypeChatEmote, TypeTwitchReward}

const (
	ActionSound      = "sound"
	ActionAnonupload = "anonupload"
)

var actions = []string{ActionSound, ActionAnonupload}

const (
	maxDataLength        = 65535
	maxEmoteIDLength     = 64
	maxTwitchRewardIDLen = 64
	mysqlDuplicateEntry  = 1062
	mysqlNoReferencedRow = 1452
	columnID             = "r.id"
	columnUserID         = "r.user_id"
)

var (
	ErrMissingType    = errors.New(`missing "type" field`)
	ErrInvalidType    = fmt.Errorf("type must be one of: %s", strings.Join(types, ", "))
	ErrMissingData    = errors.New(`missing "data" field`)
	ErrInvalidData    = errors.New("data is not valid UTF-8")
	ErrDataTooLong    = fmt.Errorf("data exceeds %d bytes", maxDataLength)
	ErrInvalidEmote   = fmt.Errorf("emote must be in the form <provider>:<emote_id>, where provider is one of: %s", strings.Join(emotes.Providers, ", "))
	ErrEmoteNotFound  = errors.New("emote is not available in the user's channel or globally")
	ErrInvalidTwitch  = errors.New("channel point reward ID is not valid")
	ErrTwitchDisabled = errors.New("twitch channel point rewards are not enabled on this instance")
	ErrTwitchNotFound = errors.New("channel point reward is not found in the user's Twitch channel")
	ErrUploadNotFound = errors.New("upload not found")
	ErrInvalidAction  = fmt.Errorf("action must be one of: %s", strings.Join(actions, ", "))
	ErrAnonDisabled   = errors.New("anonupload rewards are not enabled on this instance")
	ErrAnonType       = errors.New("anonupload rewards can only be triggered by channel point rewards")
	ErrDuplicate      = errors.New("reward with this type and data already exists")
	ErrRewardNotFound = errors.New("reward not found")
)

type Reward struct {
	ID        string         `json:"id"`
	UserID    string         `json:"user_id"`
	Type      string         `json:"type"`
	Data      string         `json:"data"`
	Action    string         `json:"action"`
	Upload    *sounds.Upload `json:"upload"`
	CreatedAt time.Time      `json:"created_at"`
}

func parseType(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", ErrMissingType
	}
	for _, known := range types {
		if t == known {
			return t, nil
		}
	}
	return "", ErrInvalidType
}

func parseAction(raw string) (string, error) {
	a := strings.TrimSpace(raw)
	if a == "" {
		return ActionSound, nil
	}
	for _, known := range actions {
		if a == known {
			return a, nil
		}
	}
	return "", ErrInvalidAction
}

func parseData(rewardType, raw string) (string, error) {
	data := strings.TrimSpace(raw)
	switch {
	case data == "":
		return "", ErrMissingData
	case !utf8.ValidString(data):
		return "", ErrInvalidData
	case len(data) > maxDataLength:
		return "", ErrDataTooLong
	}
	switch rewardType {
	case TypeChatEmote:
		provider, id, err := parseEmote(data)
		if err != nil {
			return "", err
		}
		return provider + ":" + id, nil
	case TypeTwitchReward:
		if len(data) > maxTwitchRewardIDLen || !isID(data) {
			return "", ErrInvalidTwitch
		}
	}
	return data, nil
}

func parseEmote(data string) (string, string, error) {
	provider, id, ok := strings.Cut(data, ":")
	if !ok {
		return "", "", ErrInvalidEmote
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	id = strings.TrimSpace(id)

	if !emotes.IsProvider(provider) || len(id) > maxEmoteIDLength || !isID(id) {
		return "", "", ErrInvalidEmote
	}
	return provider, id, nil
}

func isID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (h *Handler) verifyEmote(ctx context.Context, userID, data string) error {
	provider, id, err := parseEmote(data)
	if err != nil {
		return err
	}
	twitchUserID, err := h.twitchUserID(ctx, userID)
	if err != nil {
		return err
	}
	ok, err := h.opts.Emotes.Exists(ctx, provider, twitchUserID, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrEmoteNotFound
	}
	return nil
}

func (h *Handler) verifyTwitchReward(ctx context.Context, userID, rewardID string) error {
	if h.opts.Twitch == nil {
		return ErrTwitchDisabled
	}
	ok, err := h.opts.Twitch.HasReward(ctx, userID, rewardID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTwitchNotFound
	}
	return nil
}

func (h *Handler) twitchUserID(ctx context.Context, userID string) (string, error) {
	var aliasID string
	err := h.opts.DB.QueryRowContext(ctx,
		`SELECT alias_id FROM connections WHERE user_id = ? AND platform = ?`,
		userID, account.PlatformTwitch,
	).Scan(&aliasID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return aliasID, err
}

func (h *Handler) insert(ctx context.Context, id, userID, rewardType, data, action, uploadID string) error {
	var (
		res sql.Result
		err error
	)
	if action == ActionSound {
		res, err = h.opts.DB.ExecContext(ctx,
			`INSERT INTO rewards (id, user_id, type, data, action, upload_id)
			 SELECT ?, ?, ?, ?, ?, id FROM uploads WHERE id = ?`,
			id, userID, rewardType, data, action, uploadID,
		)
	} else {
		res, err = h.opts.DB.ExecContext(ctx,
			`INSERT INTO rewards (id, user_id, type, data, action, upload_id) VALUES (?, ?, ?, ?, ?, NULL)`,
			id, userID, rewardType, data, action,
		)
	}
	if isMySQLError(err, mysqlDuplicateEntry) {
		return ErrDuplicate
	}
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUploadNotFound
	}
	return nil
}

func (h *Handler) delete(ctx context.Context, id, userID string) error {
	res, err := h.opts.DB.ExecContext(ctx, `DELETE FROM rewards WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrRewardNotFound
	}
	return nil
}

func (h *Handler) find(ctx context.Context, baseURL, column, value string) ([]Reward, error) {
	items := []Reward{}

	rows, err := h.opts.DB.QueryContext(ctx,
		`SELECT r.id, r.user_id, r.type, r.data, r.action, r.created_at,
		        u.id, u.code, u.user_id, usr.username, u.uploaded_at,
		        f.id, f.mime, f.extension, f.size
		 FROM rewards r
		 LEFT JOIN uploads u ON u.id = r.upload_id
		 LEFT JOIN users usr ON usr.id = u.user_id
		 LEFT JOIN upload_files uf ON uf.upload_id = u.id
		 LEFT JOIN files f ON f.id = uf.file_id
		 WHERE `+column+` = ?
		 ORDER BY r.created_at, r.id, f.id`,
		value,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			r          Reward
			upload     sounds.Upload
			uploadID   *string
			uploadCode *string
			uploadedAt *time.Time
			fileID     *string
			mime       *string
			extension  *string
			size       *int64
		)
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Type, &r.Data, &r.Action, &r.CreatedAt,
			&uploadID, &uploadCode, &upload.UserID, &upload.Username, &uploadedAt,
			&fileID, &mime, &extension, &size,
		); err != nil {
			return nil, err
		}
		if n := len(items); n == 0 || items[n-1].ID != r.ID {
			if uploadID != nil {
				upload.ID, upload.Code, upload.UploadedAt = *uploadID, *uploadCode, *uploadedAt
				upload.Files = []sounds.File{}
				r.Upload = &upload
			}
			items = append(items, r)
		}
		if last := &items[len(items)-1]; fileID != nil && last.Upload != nil {
			last.Upload.Files = append(last.Upload.Files, sounds.File{
				ID:        *fileID,
				Mime:      *mime,
				Extension: *extension,
				Size:      *size,
				URL:       sounds.FileURL(baseURL, *fileID, *extension),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (h *Handler) actionFor(ctx context.Context, userID, rewardType, data string) (string, error) {
	var action string
	err := h.opts.DB.QueryRowContext(ctx,
		`SELECT action FROM rewards WHERE user_id = ? AND type = ? AND data_hash = UNHEX(SHA2(?, 256))`,
		userID, rewardType, data,
	).Scan(&action)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return action, err
}

func (h *Handler) userExists(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := h.opts.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, userID).Scan(&exists)
	return exists, err
}

func isMySQLError(err error, number uint16) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == number
}
