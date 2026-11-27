package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/t0mer/mashov2go/internal/family"
)

// CreateMember inserts a member after the caller has validated it.
func (s *Store) CreateMember(ctx context.Context, m family.Member) (family.Member, error) {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO members(role, name, language, whatsapp_phone, telegram_chat_id, channel, whatsapp_connector_id,
		  fallback_channel, quiet_start, quiet_end, plan_enabled, plan_time, include_homework_today, active, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.Role, m.Name, m.Language, m.WhatsAppPhone, nullInt(m.TelegramChatID), m.Channel, nullInt(m.WhatsAppConnectorID),
		nullStr(m.FallbackChannel), m.QuietStart, m.QuietEnd, boolInt(m.PlanEnabled), m.PlanTime,
		boolInt(m.IncludeHomeworkToday), boolInt(m.Active), m.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return m, err
	}
	m.ID, err = res.LastInsertId()
	return m, err
}

// UpdateMember overwrites a member's editable fields.
func (s *Store) UpdateMember(ctx context.Context, m family.Member) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE members SET name=?, language=?, whatsapp_phone=?, telegram_chat_id=?, channel=?, whatsapp_connector_id=?,
		  fallback_channel=?, quiet_start=?, quiet_end=?, plan_enabled=?, plan_time=?, include_homework_today=?, active=?
		WHERE id=? AND deleted_at IS NULL`,
		m.Name, m.Language, m.WhatsAppPhone, nullInt(m.TelegramChatID), m.Channel, nullInt(m.WhatsAppConnectorID),
		nullStr(m.FallbackChannel), m.QuietStart, m.QuietEnd, boolInt(m.PlanEnabled), m.PlanTime,
		boolInt(m.IncludeHomeworkToday), boolInt(m.Active), m.ID)
	return err
}

// GetMember loads a live (not deleted) member.
func (s *Store) GetMember(ctx context.Context, id int64) (family.Member, error) {
	return scanMember(s.db.QueryRowContext(ctx, memberSelect+` WHERE id = ? AND deleted_at IS NULL`, id))
}

// ListMembers returns live members, parents and children, in creation order.
func (s *Store) ListMembers(ctx context.Context) ([]family.Member, error) {
	rows, err := s.db.QueryContext(ctx, memberSelect+` WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []family.Member
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMember soft-deletes a member and removes its child link and preferences.
func (s *Store) DeleteMember(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM member_links WHERE member_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM delivery_prefs WHERE child_member_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM parent_mutes WHERE parent_member_id = ? OR child_member_id = ?`, id, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE members SET deleted_at = ?, active = 0 WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id); err != nil {
		return err
	}
	return tx.Commit()
}

// LinkChild ties a child member to a Mashov student. A student can be linked once.
func (s *Store) LinkChild(ctx context.Context, memberID, accountID int64, childGUID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO member_links(member_id, account_id, child_guid) VALUES(?,?,?)`,
		memberID, accountID, childGUID)
	return err
}

// ChildMemberFor returns the child member linked to a Mashov student.
func (s *Store) ChildMemberFor(ctx context.Context, accountID int64, childGUID string) (family.Member, error) {
	m, err := scanMember(s.db.QueryRowContext(ctx, `
		SELECT m.id, m.role, m.name, m.language, m.whatsapp_phone, m.telegram_chat_id, m.channel, m.whatsapp_connector_id,
		       m.fallback_channel, m.quiet_start, m.quiet_end, m.plan_enabled, m.plan_time, m.include_homework_today, m.active, m.created_at
		FROM members m JOIN member_links l ON l.member_id = m.id
		WHERE l.account_id = ? AND l.child_guid = ? AND m.deleted_at IS NULL`, accountID, childGUID))
	return m, notFound(err)
}

// SetRecipient stores the recipient for one category of a child.
func (s *Store) SetRecipient(ctx context.Context, childMemberID int64, category string, r family.Recipient) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO delivery_prefs(child_member_id, category, recipient) VALUES(?,?,?)
		ON CONFLICT(child_member_id, category) DO UPDATE SET recipient = excluded.recipient`,
		childMemberID, category, string(r))
	return err
}

// Recipients returns the stored overrides for a child. Categories not present
// use family.DefaultRecipients.
func (s *Store) Recipients(ctx context.Context, childMemberID int64) (map[string]family.Recipient, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT category, recipient FROM delivery_prefs WHERE child_member_id = ?`, childMemberID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]family.Recipient{}
	for rows.Next() {
		var c, r string
		if err := rows.Scan(&c, &r); err != nil {
			return nil, err
		}
		out[c] = family.Recipient(r)
	}
	return out, rows.Err()
}

// SetMute silences (or un-silences) a category for one parent on one child.
func (s *Store) SetMute(ctx context.Context, parentID, childID int64, category string, muted bool) error {
	if !muted {
		_, err := s.db.ExecContext(ctx, `DELETE FROM parent_mutes WHERE parent_member_id=? AND child_member_id=? AND category=?`,
			parentID, childID, category)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO parent_mutes(parent_member_id, child_member_id, category) VALUES(?,?,?)`,
		parentID, childID, category)
	return err
}

// IsMuted reports whether a parent has muted a category for a child.
func (s *Store) IsMuted(ctx context.Context, parentID, childID int64, category string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM parent_mutes WHERE parent_member_id=? AND child_member_id=? AND category=?`,
		parentID, childID, category).Scan(&n)
	return n > 0, err
}

const memberSelect = `SELECT id, role, name, language, whatsapp_phone, telegram_chat_id, channel, whatsapp_connector_id,
	fallback_channel, quiet_start, quiet_end, plan_enabled, plan_time, include_homework_today, active, created_at FROM members`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanMember(r rowScanner) (family.Member, error) {
	var m family.Member
	var tg, wc sql.NullInt64
	var fb sql.NullString
	var plan, hw, active int
	var created string
	err := r.Scan(&m.ID, &m.Role, &m.Name, &m.Language, &m.WhatsAppPhone, &tg, &m.Channel, &wc, &fb,
		&m.QuietStart, &m.QuietEnd, &plan, &m.PlanTime, &hw, &active, &created)
	if err != nil {
		return m, notFound(err)
	}
	m.TelegramChatID, m.WhatsAppConnectorID, m.FallbackChannel = tg.Int64, wc.Int64, fb.String
	m.PlanEnabled, m.IncludeHomeworkToday, m.Active = plan == 1, hw == 1, active == 1
	m.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return m, nil
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// MemberLink returns the Mashov account and student a child member is linked to.
// ok is false when the member has no link.
func (s *Store) MemberLink(ctx context.Context, memberID int64) (accountID int64, childGUID string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT account_id, child_guid FROM member_links WHERE member_id = ?`, memberID).
		Scan(&accountID, &childGUID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return accountID, childGUID, true, nil
}

// UnlinkChild removes a child member's Mashov student link. It is a no-op when
// the member has no link.
func (s *Store) UnlinkChild(ctx context.Context, memberID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM member_links WHERE member_id = ?`, memberID)
	return err
}

// ResetRecipients drops a child's recipient overrides so the defaults apply again.
func (s *Store) ResetRecipients(ctx context.Context, childMemberID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM delivery_prefs WHERE child_member_id = ?`, childMemberID)
	return err
}

// Mute is one silenced category for a parent on a child.
type Mute struct {
	ChildID  int64
	Category string
}

// ListMutes returns the mutes a parent has set.
func (s *Store) ListMutes(ctx context.Context, parentID int64) ([]Mute, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT child_member_id, category FROM parent_mutes WHERE parent_member_id = ? ORDER BY child_member_id, category`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mute
	for rows.Next() {
		var m Mute
		if err := rows.Scan(&m.ChildID, &m.Category); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ReplaceMutes sets the parent's mutes to exactly the given list, in one transaction.
func (s *Store) ReplaceMutes(ctx context.Context, parentID int64, mutes []Mute) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM parent_mutes WHERE parent_member_id = ?`, parentID); err != nil {
		return err
	}
	for _, m := range mutes {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO parent_mutes(parent_member_id, child_member_id, category) VALUES(?,?,?)`,
			parentID, m.ChildID, m.Category); err != nil {
			return err
		}
	}
	return tx.Commit()
}
